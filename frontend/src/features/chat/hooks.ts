import { useCallback, useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { ApiError } from "@/api/client";
import { toast } from "@/components/ui/sonner";
import { dailyInputKey } from "@/features/daily/hooks";
import { entriesKey } from "@/features/journal/hooks";

import {
  type ChatMessage,
  type ChatPhase,
  type ChatSession,
  type ChatSessionEnvelope,
  type ChatStreamEvent,
  type ExtractionStatus,
  type ExtractionStatusResponse,
  type FinalizeResponse,
  createOrResumeSession,
  finalizeSession,
  getExtractionStatus,
  getSessionByDate,
  getTodaySession,
  resetSession,
  streamMessage,
  streamOpener,
  streamWrapUp,
} from "./api";

// ---------- query keys ----------

// Today's chat session (singleton). Past sessions are keyed by date so
// HistoryView can cache transcripts immutably.
export const chatSessionKey = (date?: string) =>
  date ? (["chat", date] as const) : (["chat", "today"] as const);

export const chatExtractionStatusKey = (sessionId: string) =>
  ["chat", "extraction-status", sessionId] as const;

// Stats key — local copy so we don't pull the entire summaries module.
const STATS_KEY = (days: number) => ["summaries", "stats", days] as const;

// ---------- session queries ----------

export function useTodayChatSession() {
  return useQuery<ChatSessionEnvelope, ApiError>({
    queryKey: chatSessionKey(),
    queryFn: getTodaySession,
    staleTime: 60_000,
  });
}

export function useChatByDate(date: string | null) {
  return useQuery<ChatSessionEnvelope, ApiError>({
    queryKey: chatSessionKey(date ?? "none"),
    queryFn: () => getSessionByDate(date as string),
    enabled: !!date,
    staleTime: Infinity, // past days are immutable
  });
}

// ---------- create-or-resume ----------

export function useCreateOrResumeSession() {
  const qc = useQueryClient();
  return useMutation<ChatSessionEnvelope, ApiError, void>({
    mutationFn: createOrResumeSession,
    onSuccess: (env) => {
      qc.setQueryData<ChatSessionEnvelope>(chatSessionKey(), env);
    },
  });
}

// ---------- streaming state machine ----------

export type StreamStatus = "idle" | "streaming" | "done" | "error";

export interface StreamingState {
  status: StreamStatus;
  // Live-accumulating assistant content (cleared when the assistant
  // message lands in cache).
  partial: string;
  // Tool calls observed in this run; used by the coverage chips.
  toolEvents: { name: string; args: Record<string, unknown> }[];
  // If the server emits a phase frame (e.g. propose_wrap_up), the FE
  // updates the cached session.phase for immediate UI response.
  lastError: string | null;
  // Crisis state — set when the safety regex fires; UI swaps the
  // composer for the crisis card.
  crisis: { reason: string; resources_url: string } | null;
}

const initialStreamState: StreamingState = {
  status: "idle",
  partial: "",
  toolEvents: [],
  lastError: null,
  crisis: null,
};

export interface UseStreamingChatResult {
  state: StreamingState;
  sendMessage: (content: string) => Promise<void>;
  startOpener: () => Promise<void>;
  triggerWrapUp: () => Promise<void>;
  abort: () => void;
  dismissCrisis: () => void;
  // clearPartial drops the in-flight streaming text. Consumers call this
  // once they've observed the persisted assistant turn arrive in the
  // messages cache — the streaming bubble (driven by partial) stays
  // mounted until then so there's no flicker between stream-ended and
  // refetch-completed.
  clearPartial: () => void;
}

// useStreamingChat is the streaming state machine for the active session.
// Wraps the SSE generator and keeps a derived view (partial text, tool
// events, crisis flag) the components render directly.
//
// Concurrency: only one stream at a time. A second sendMessage call
// while status='streaming' is rejected (the composer disables itself).
export function useStreamingChat(sessionId: string | null): UseStreamingChatResult {
  const qc = useQueryClient();
  const [state, setState] = useState<StreamingState>(initialStreamState);
  const abortRef = useRef<AbortController | null>(null);

  // Reset state when the session id changes (e.g. between days).
  useEffect(() => {
    setState(initialStreamState);
    abortRef.current?.abort();
    abortRef.current = null;
  }, [sessionId]);

  const consumeStream = useCallback(
    async (gen: AsyncGenerator<ChatStreamEvent>) => {
      try {
        for await (const ev of gen) {
          switch (ev.kind) {
            case "token":
              setState((s) => ({ ...s, partial: s.partial + ev.delta }));
              break;
            case "tool":
              setState((s) => ({
                ...s,
                toolEvents: [...s.toolEvents, { name: ev.name, args: ev.args }],
              }));
              break;
            case "phase":
              // Update cached session.phase so the composer / wrap-up
              // affordance re-renders without a refetch.
              qc.setQueryData<ChatSessionEnvelope>(chatSessionKey(), (old) => {
                if (!old?.session) return old;
                return { ...old, session: { ...old.session, phase: ev.phase } };
              });
              break;
            case "coverage_update":
              // Authoritative covered set from the post-turn classifier.
              // Write straight into the session cache so consumers
              // (CoverageChips) read the same source pre/post-stream.
              qc.setQueryData<ChatSessionEnvelope>(chatSessionKey(), (old) => {
                if (!old?.session) return old;
                return {
                  ...old,
                  session: {
                    ...old.session,
                    covered_question_ids: ev.covered_question_ids,
                  },
                };
              });
              break;
            case "crisis":
              setState((s) => ({
                ...s,
                status: "error",
                crisis: { reason: ev.reason, resources_url: ev.resources_url },
              }));
              return;
            case "error":
              setState((s) => ({ ...s, status: "error", lastError: ev.message }));
              return;
            case "done":
              setState((s) => ({ ...s, status: "done" }));
              // Refetch the session envelope. We deliberately do NOT
              // clear `partial` here — that's the consumer's job, via
              // clearPartial(), once it sees the persisted assistant
              // turn land in the messages array. Clearing eagerly was
              // the source of the flicker where the bubble briefly
              // disappeared and reappeared.
              qc.invalidateQueries({ queryKey: chatSessionKey() });
              // NOTE: don't return — the server emits `done` BEFORE
              // the post-turn coverage classifier so the composer
              // re-enables immediately. A `coverage_update` frame may
              // still arrive on the same connection; keep iterating
              // until the generator naturally ends.
              break;
          }
        }
        // Stream ended naturally (post-`done` close, or server closed
        // early without a `done` frame). Flip to 'done' if we were
        // still streaming so the composer doesn't get stuck. Don't
        // clear partial here — the consumer's clearPartial will fire
        // once the persisted message arrives.
        setState((s) => ({ ...s, status: s.status === "streaming" ? "done" : s.status }));
      } catch (err) {
        if ((err as { name?: string })?.name === "AbortError") {
          setState((s) => ({ ...s, status: "idle", partial: "" }));
          return;
        }
        const msg = err instanceof Error ? err.message : "stream failed";
        setState((s) => ({ ...s, status: "error", lastError: msg }));
        toast.error("Couldn't reach the assistant", { description: msg });
      }
    },
    [qc],
  );

  const sendMessage = useCallback(
    async (content: string) => {
      if (!sessionId) return;
      if (state.status === "streaming") return;
      abortRef.current?.abort();
      const ac = new AbortController();
      abortRef.current = ac;
      setState({ ...initialStreamState, status: "streaming" });

      // Optimistic: append the user message to the cached transcript so
      // the bubble appears immediately.
      qc.setQueryData<ChatSessionEnvelope>(chatSessionKey(), (old) => {
        if (!old?.session) return old;
        const optimisticMsg: ChatMessage = {
          id: `optimistic-${Date.now()}`,
          session_id: old.session.id,
          seq: (old.messages[old.messages.length - 1]?.seq ?? 0) + 1,
          role: "user",
          content,
          token_in: 0,
          token_out: 0,
          created_at: new Date().toISOString(),
        };
        return { ...old, messages: [...old.messages, optimisticMsg] };
      });

      try {
        await consumeStream(streamMessage(sessionId, content, ac.signal));
      } catch (err) {
        const msg = err instanceof Error ? err.message : "send failed";
        setState((s) => ({ ...s, status: "error", lastError: msg }));
        // Roll back optimistic write — refetch will resolve.
        qc.invalidateQueries({ queryKey: chatSessionKey() });
        toast.error("Couldn't send", { description: msg });
      }
    },
    [sessionId, state.status, qc, consumeStream],
  );

  const startOpener = useCallback(async () => {
    if (!sessionId) return;
    if (state.status === "streaming") return;
    abortRef.current?.abort();
    const ac = new AbortController();
    abortRef.current = ac;
    setState({ ...initialStreamState, status: "streaming" });
    try {
      await consumeStream(streamOpener(sessionId, ac.signal));
    } catch (err) {
      const msg = err instanceof Error ? err.message : "opener failed";
      setState((s) => ({ ...s, status: "error", lastError: msg }));
    }
  }, [sessionId, state.status, consumeStream]);

  // triggerWrapUp fires the user-initiated closing pass. The server
  // appends a system_event ("user_wrap_up"), advances phase to
  // wrapping_up, and streams a single assistant turn that covers any
  // remaining topics and proposes wrap-up. Same SSE consumer as a
  // normal message.
  const triggerWrapUp = useCallback(async () => {
    if (!sessionId) return;
    if (state.status === "streaming") return;
    abortRef.current?.abort();
    const ac = new AbortController();
    abortRef.current = ac;
    setState({ ...initialStreamState, status: "streaming" });
    try {
      await consumeStream(streamWrapUp(sessionId, ac.signal));
    } catch (err) {
      const msg = err instanceof Error ? err.message : "wrap-up failed";
      setState((s) => ({ ...s, status: "error", lastError: msg }));
      qc.invalidateQueries({ queryKey: chatSessionKey() });
      toast.error("Couldn't wrap up", { description: msg });
    }
  }, [sessionId, state.status, qc, consumeStream]);

  const abort = useCallback(() => {
    abortRef.current?.abort();
    abortRef.current = null;
    setState({ ...initialStreamState });
  }, []);

  const dismissCrisis = useCallback(() => {
    setState((s) => ({ ...s, status: "idle", crisis: null, partial: "" }));
  }, []);

  const clearPartial = useCallback(() => {
    setState((s) => (s.partial ? { ...s, partial: "" } : s));
  }, []);

  // Cleanup on unmount.
  useEffect(() => {
    return () => {
      abortRef.current?.abort();
    };
  }, []);

  return { state, sendMessage, startOpener, triggerWrapUp, abort, dismissCrisis, clearPartial };
}

// ---------- finalize + extraction polling ----------

export function useFinalizeChat() {
  const qc = useQueryClient();
  return useMutation<FinalizeResponse, ApiError, string>({
    mutationFn: (sessionId) => finalizeSession(sessionId),
    onSuccess: (resp) => {
      // Optimistically reflect the new phase + extraction_status so the
      // sticky-bar "Updating…" pill appears immediately, before the
      // 2s polling interval has a chance to round-trip.
      qc.setQueryData<ChatSessionEnvelope>(chatSessionKey(), (old) => {
        if (!old?.session) return old;
        return {
          ...old,
          session: {
            ...old.session,
            phase: resp.phase,
            extraction_status: resp.extraction_status,
          },
        };
      });
      qc.invalidateQueries({ queryKey: chatExtractionStatusKey(resp.session_id) });
    },
  });
}

// useExtractionStatus polls every 2s while the session is pending or
// running, and stops on completed/failed. On `completed`, invalidates
// daily-input + entries + stats caches so the manual fields update.
// The chat itself stays open — extraction is a "refresh the check-in"
// trigger, not a session terminator.
export function useExtractionStatus(sessionId: string | null, enabled: boolean) {
  const qc = useQueryClient();
  const seenCompleted = useRef(false);

  const query = useQuery<ExtractionStatusResponse, ApiError>({
    queryKey: chatExtractionStatusKey(sessionId ?? ""),
    queryFn: () => getExtractionStatus(sessionId as string),
    enabled: !!sessionId && enabled,
    refetchInterval: (q) => {
      const s = q.state.data?.status;
      if (s === "pending" || s === "running") return 2_000;
      return false;
    },
    refetchIntervalInBackground: false,
  });

  useEffect(() => {
    const status = query.data?.status;
    if (!sessionId || !status) return;
    if (status === "completed" && !seenCompleted.current) {
      seenCompleted.current = true;
      qc.invalidateQueries({ queryKey: chatSessionKey() });
      qc.invalidateQueries({ queryKey: dailyInputKey() });
      qc.invalidateQueries({ queryKey: entriesKey() });
      qc.invalidateQueries({ queryKey: STATS_KEY(90) });
      toast.success("Check-in updated", {
        description: "Mood, emotions, and answers refreshed from the conversation. Keep chatting whenever.",
      });
    }
    if (status === "failed") {
      seenCompleted.current = false;
      toast.error("Couldn't update the check-in", {
        description:
          query.data?.error ??
          "Try again, or switch to Manual to edit by hand.",
      });
    }
  }, [query.data?.status, query.data?.error, sessionId, qc]);

  return query;
}

// useResetChat performs the destructive reset. The FE shows a
// confirmation dialog (ChatHeader) before calling. On success, the
// session envelope returns to greeting phase with no messages and the
// opener auto-streams again.
export function useResetChat() {
  const qc = useQueryClient();
  return useMutation<ChatSessionEnvelope, ApiError, string>({
    mutationFn: (sessionId) => resetSession(sessionId),
    onSuccess: (env) => {
      qc.setQueryData<ChatSessionEnvelope>(chatSessionKey(), env);
      qc.invalidateQueries({ queryKey: chatSessionKey() });
    },
    onError: (err) => {
      toast.error("Couldn't reset", { description: err.message });
    },
  });
}

// ---------- helpers ----------

export function isActivePhase(phase: ChatPhase | undefined): boolean {
  return phase === "greeting" || phase === "exploring" || phase === "wrapping_up";
}

export function isExtractionInFlight(status: ExtractionStatus | undefined): boolean {
  return status === "pending" || status === "running";
}

export function lastAssistantMessage(messages: ChatMessage[]): ChatMessage | undefined {
  for (let i = messages.length - 1; i >= 0; i--) {
    if (messages[i].role === "assistant") return messages[i];
  }
  return undefined;
}

export function visibleMessages(messages: ChatMessage[]): ChatMessage[] {
  // Hide tool + system_event rows from the bubble list — they're
  // surfaced as chips / banners elsewhere.
  return messages.filter((m) => m.role === "user" || m.role === "assistant");
}

// Re-exports for ergonomic imports from components.
export type { ChatMessage, ChatSession, ChatSessionEnvelope, ChatPhase };
