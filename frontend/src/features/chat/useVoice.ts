// React glue for VoiceController. Owns one controller instance per
// mount; exposes status + start/stop and pumps a module-level "voice
// live" flag so the Chat tab can render read-only while a call is in
// progress.
//
// useSyncExternalStore avoids prop-drilling the live state through
// AppShell; the flag is global to the tab (only one voice call runs at
// a time anyway).

import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { toast } from "@/components/ui/sonner";

import { type ChatMessage, type ChatSessionEnvelope } from "./api";
import { chatSessionKey } from "./hooks";
import { VoiceController, type VoiceStatus } from "./voice";

// ---------- module-level live flag ----------

let voiceLive = false;
const liveListeners = new Set<() => void>();

function setVoiceLive(next: boolean) {
  if (voiceLive === next) return;
  voiceLive = next;
  liveListeners.forEach((fn) => fn());
}

function subscribeVoiceLive(fn: () => void) {
  liveListeners.add(fn);
  return () => {
    liveListeners.delete(fn);
  };
}

function getVoiceLive() {
  return voiceLive;
}

// useVoiceLive returns true when a Talk call is currently connecting or
// live. Used by ChatPanel to hide its composer.
export function useVoiceLive(): boolean {
  return useSyncExternalStore(subscribeVoiceLive, getVoiceLive, () => false);
}

// ---------- the hook ----------

export interface UseVoiceResult {
  status: VoiceStatus;
  lastError: string | null;
  start: () => Promise<void>;
  stop: () => Promise<void>;
}

export function useVoice(sessionId: string | null): UseVoiceResult {
  const qc = useQueryClient();
  const [status, setStatus] = useState<VoiceStatus>("idle");
  const [lastError, setLastError] = useState<string | null>(null);
  const controllerRef = useRef<VoiceController | null>(null);

  // Lazily build the controller on first start; rebuild after stop so
  // listener closures stay fresh w.r.t. sessionId.
  const ensureController = useMemo(() => {
    return () => {
      if (controllerRef.current) return controllerRef.current;
      const ctrl = new VoiceController({
        onStatusChange: (s) => {
          setStatus(s);
          setVoiceLive(s === "connecting" || s === "live");
        },
        onError: (msg) => {
          setLastError(msg);
          toast.error("Voice", { description: msg });
        },
        onTranscript: ({ role, content }) => {
          // Optimistically merge into the today-session cache so the
          // bubble appears before the next refetch lands. The server
          // assigns the canonical seq; we use a temporary one here.
          qc.setQueryData<ChatSessionEnvelope>(chatSessionKey(), (old) => {
            if (!old?.session) return old;
            const last = old.messages[old.messages.length - 1];
            const optimistic: ChatMessage = {
              id: `voice-optimistic-${Date.now()}-${Math.random()}`,
              session_id: old.session.id,
              seq: (last?.seq ?? 0) + 1,
              role,
              content,
              token_in: 0,
              token_out: 0,
              created_at: new Date().toISOString(),
            };
            return { ...old, messages: [...old.messages, optimistic] };
          });
          // Schedule a background refetch so the optimistic row is
          // replaced by the canonical persisted row.
          qc.invalidateQueries({ queryKey: chatSessionKey() });
        },
        onCrisis: () => {
          toast.error("Crisis support", {
            description:
              "We've paused the call. If you need help right now, see /resources.",
          });
        },
      });
      controllerRef.current = ctrl;
      return ctrl;
    };
  }, [qc]);

  const start = async () => {
    if (!sessionId) return;
    setLastError(null);
    const ctrl = ensureController();
    await ctrl.start(sessionId);
  };

  const stop = async () => {
    const ctrl = controllerRef.current;
    if (!ctrl) return;
    await ctrl.stop();
    controllerRef.current = null;
  };

  // Cleanup on unmount.
  useEffect(() => {
    return () => {
      const ctrl = controllerRef.current;
      if (ctrl) {
        void ctrl.stop();
      }
      controllerRef.current = null;
      setVoiceLive(false);
    };
  }, []);

  return { status, lastError, start, stop };
}
