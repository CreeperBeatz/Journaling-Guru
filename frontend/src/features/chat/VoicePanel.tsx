import { useEffect } from "react";
import { Loader2, Mic, MicOff } from "lucide-react";

import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

import { type ChatSession } from "./api";
import {
  useCreateOrResumeSession,
  useFinalizeChat,
  useTodayChatSession,
} from "./hooks";
import { useVoice } from "./useVoice";

// VoicePanel is the body of the Talk tab. The whole surface is a single
// big mic toggle — no live transcript, no keep/replace finalize fork.
// The model still talks back via audio (hidden <audio> element wired in
// VoiceController). When the user taps Done after speaking, finalize
// runs the existing extraction worker, which silently merges chat-
// extracted answers into any pre-existing manual entries (LLM-merge).
export function VoicePanel() {
  const sessionQuery = useTodayChatSession();
  const createOrResume = useCreateOrResumeSession();
  const finalize = useFinalizeChat();

  const session: ChatSession | null = sessionQuery.data?.session ?? null;
  const voice = useVoice(session?.id ?? null);

  useEffect(() => {
    if (sessionQuery.isPending) return;
    if (sessionQuery.data && !sessionQuery.data.session) {
      createOrResume.mutate();
    }
  }, [sessionQuery.isPending, sessionQuery.data, createOrResume]);

  // Stop the call on unmount (defense-in-depth alongside useVoice's own
  // cleanup, for tab swaps).
  useEffect(() => {
    return () => {
      void voice.stop();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

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

  const isLive = voice.status === "live";
  const isConnecting = voice.status === "connecting";
  const isEnding = voice.status === "ending";
  const isSaving = finalize.isPending;

  const statusLabel = isSaving
    ? "Saving your reflections…"
    : voice.status === "idle"
      ? "Tap to start talking"
      : voice.status === "connecting"
        ? "Connecting…"
        : voice.status === "live"
          ? "Listening — speak whenever"
          : "Ending…";

  const handleToggle = () => {
    if (voice.status === "idle") {
      void voice.start();
    } else {
      void voice.stop();
    }
  };

  const handleDone = async () => {
    if (!session) return;
    if (voice.status !== "idle") {
      await voice.stop();
    }
    finalize.mutate({ sessionId: session.id });
  };

  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardContent className="px-4 py-12 md:px-6 md:py-16">
          <div className="flex flex-col items-center gap-6">
            <button
              type="button"
              onClick={handleToggle}
              disabled={isEnding || isSaving}
              className={cn(
                "relative flex h-32 w-32 items-center justify-center rounded-full",
                "border border-border/60 transition-all",
                isLive
                  ? "bg-destructive text-destructive-foreground shadow-lg ring-4 ring-destructive/20 animate-pulse"
                  : "bg-accent text-accent-foreground hover:bg-accent/85",
                isConnecting && "animate-pulse",
                (isEnding || isSaving) && "opacity-60",
              )}
              aria-label={isLive ? "Stop talking" : "Start talking"}
            >
              {isConnecting || isSaving ? (
                <Loader2 className="h-12 w-12 animate-spin" aria-hidden />
              ) : isLive ? (
                <MicOff className="h-12 w-12" aria-hidden />
              ) : (
                <Mic className="h-12 w-12" aria-hidden />
              )}
            </button>
            <p className="text-sm text-muted-foreground" aria-live="polite">
              {statusLabel}
            </p>
            {voice.lastError ? (
              <p className="text-xs text-destructive">{voice.lastError}</p>
            ) : null}
            <Button
              variant="outline"
              size="sm"
              onClick={handleDone}
              disabled={isSaving || isConnecting}
            >
              {isSaving ? "Saving…" : "Done — save to today's check-in"}
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
