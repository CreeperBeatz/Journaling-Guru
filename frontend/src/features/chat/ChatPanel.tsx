import { useEffect, useMemo } from "react";

import { Card, CardContent } from "@/components/ui/card";
import { useQuestions } from "@/features/journal/hooks";

import {
  type ChatMessage,
  type ChatPhase,
  type ChatSession,
} from "./api";
import {
  useCreateOrResumeSession,
  useFinalizeChat,
  useResetChat,
  useStreamingChat,
  useTodayChatSession,
  visibleMessages,
} from "./hooks";
import { ChatHeader } from "./components/ChatHeader";
import { ComposerInput } from "./components/ComposerInput";
import { CoverageChips } from "./components/CoverageChips";
import { CrisisCard } from "./components/CrisisCard";
import { MessageList } from "./components/MessageList";
import { WrapUpAffordance } from "./components/WrapUpAffordance";

// ChatPanel is the chat-mode body of /today. Composes the session
// query, streaming state machine, and finalize flow into one self-
// contained surface.
//
// Lifecycle:
//   1. Mount → useTodayChatSession; if null, immediately POST /sessions.
//   2. If session has zero messages and phase=greeting → start opener.
//   3. User sends → useStreamingChat.sendMessage drives the SSE.
//   4. propose_wrap_up tool → state.session.phase advances to
//      wrapping_up → WrapUpAffordance appears.
//   5. User clicks "Update check-in" → finalize fires; the chat stays
//      open and interactive while the extraction worker runs in the
//      background. Status surfaces as a pill in DailyEntry's sticky
//      Today bar (driven by the same polling hook).
//   6. On extraction completed, caches invalidate; the user can keep
//      chatting — new messages typed during the update simply weren't
//      part of the snapshot that just landed.
export function ChatPanel() {
  const sessionQuery = useTodayChatSession();
  const createOrResume = useCreateOrResumeSession();
  const finalize = useFinalizeChat();
  const resetChat = useResetChat();
  const questionsQuery = useQuestions();

  const session: ChatSession | null = sessionQuery.data?.session ?? null;
  const messages: ChatMessage[] = sessionQuery.data?.messages ?? [];

  const stream = useStreamingChat(session?.id ?? null);

  // Auto-create the session on first mount if none exists yet.
  useEffect(() => {
    if (sessionQuery.isPending) return;
    if (sessionQuery.data && !sessionQuery.data.session) {
      createOrResume.mutate();
    }
  }, [sessionQuery.isPending, sessionQuery.data, createOrResume]);

  // Auto-stream the opener once we have a fresh greeting-phase session
  // with no assistant messages yet. Triggered exactly once per session
  // — once the opener lands, the phase advances on the FE via the
  // streaming SSE invalidate, so this guard stays false on rerenders.
  useEffect(() => {
    if (!session) return;
    if (session.phase !== "greeting") return;
    if (messages.some((m) => m.role === "assistant")) return;
    if (stream.state.status !== "idle") return;
    void stream.startOpener();
    // Intentional: only re-fire when the session id changes; state.status
    // changes are handled internally by useStreamingChat.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session?.id, session?.phase, messages.length]);

  const visibleMsgs = useMemo(() => visibleMessages(messages), [messages]);

  // Coverage chips read the authoritative set written by the post-turn
  // classifier — it lands on session.covered_question_ids both at page
  // load (from the GET /sessions/today envelope) and live during a
  // stream (the coverage_update SSE handler patches the cache directly).
  const coveredIds = useMemo(() => {
    return new Set<string>(session?.covered_question_ids ?? []);
  }, [session?.covered_question_ids]);

  if (sessionQuery.isPending) {
    return (
      <Card>
        <CardContent className="px-6 py-12 text-center text-sm text-muted-foreground">
          Loading conversation…
        </CardContent>
      </Card>
    );
  }
  if (sessionQuery.isError) {
    return (
      <Card>
        <CardContent className="px-6 py-8 text-sm text-destructive">
          Couldn&apos;t load chat: {sessionQuery.error.message}
        </CardContent>
      </Card>
    );
  }
  if (!session) {
    return (
      <Card>
        <CardContent className="px-6 py-8 text-sm text-muted-foreground">
          Starting a new conversation…
        </CardContent>
      </Card>
    );
  }

  const phase: ChatPhase = session.phase;
  // Composer disabled only while a stream is in-flight or a crisis
  // dialog is up. The chat is never "closed" — the user can resume
  // anytime, including after a previous "Update check-in".
  const composerDisabled = stream.state.status === "streaming";
  const handleFinalize = () => {
    if (!session) return;
    finalize.mutate(session.id);
  };
  const handleReset = () => {
    if (!session) return;
    resetChat.mutate(session.id);
  };

  const userTurnCount = visibleMsgs.filter((m) => m.role === "user").length;
  const hasUserTurns = userTurnCount > 0;

  return (
    <Card className="overflow-x-clip">
      <CardContent className="flex flex-col gap-4 px-5 py-5">
        <ChatHeader
          phase={phase}
          hasUserTurns={hasUserTurns}
          hasMessages={visibleMsgs.length > 0}
          finalizePending={finalize.isPending}
          resetPending={resetChat.isPending}
          onFinalize={handleFinalize}
          onReset={handleReset}
        />

        {/* Sticky coverage strip — pins below the DailyEntry tab strip.
         * Top = AppShell mobile header (CSS var, 0 on desktop) +
         *       tab strip height (h-9 + py-2 ≈ 2.75rem). */}
        <div
          style={{ top: "calc(var(--app-mobile-header-h, 0px) + 2.75rem)" }}
          className="sticky z-10 -mx-5 border-b border-border/60 bg-background/85 px-5 py-1.5 backdrop-blur-md"
        >
          <CoverageChips
            questions={questionsQuery.data ?? []}
            coveredIds={coveredIds}
          />
        </div>

        <MessageList
          messages={visibleMsgs}
          partial={stream.state.partial}
          streaming={stream.state.status === "streaming"}
        />

        {stream.state.crisis ? (
          <CrisisCard
            resourcesUrl={stream.state.crisis.resources_url}
            onResume={stream.dismissCrisis}
          />
        ) : null}

        {phase === "wrapping_up" ? (
          <WrapUpAffordance onFinalize={handleFinalize} pending={finalize.isPending} />
        ) : null}

        <ComposerInput
          onSend={stream.sendMessage}
          disabled={composerDisabled || stream.state.crisis !== null}
          pending={stream.state.status === "streaming"}
          placeholder={placeholderForPhase(phase)}
        />

        {stream.state.lastError ? (
          <p className="text-xs text-destructive/80">
            {stream.state.lastError}
          </p>
        ) : null}
      </CardContent>
    </Card>
  );
}

function placeholderForPhase(phase: ChatPhase): string {
  switch (phase) {
    case "greeting":
      return "Type your reply…";
    case "wrapping_up":
      return "One last thing on your mind?";
    default:
      return "Say something…";
  }
}
