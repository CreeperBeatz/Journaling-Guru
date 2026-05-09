import { useEffect, useMemo, useState } from "react";
import { Loader2, Mic, MicOff } from "lucide-react";

import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

import { type ChatMessage, type ChatSession } from "./api";
import {
  useCreateOrResumeSession,
  useFinalizeChat,
  useTodayChatSession,
  visibleMessages,
} from "./hooks";
import { MessageList } from "./components/MessageList";
import { WrapUpAffordance } from "./components/WrapUpAffordance";
import { useVoice } from "./useVoice";

// VoicePanel is the body of the Talk tab. Mirrors ChatPanel's structure
// (transcript above, control affordance below) but the input surface is
// a single big mic toggle. The shared chat_sessions row keeps text/
// voice in sync — switching between Chat and Talk shows the same
// transcript.
export function VoicePanel() {
  const sessionQuery = useTodayChatSession();
  const createOrResume = useCreateOrResumeSession();
  const finalize = useFinalizeChat();

  const session: ChatSession | null = sessionQuery.data?.session ?? null;
  const messages: ChatMessage[] = sessionQuery.data?.messages ?? [];
  const voice = useVoice(session?.id ?? null);

  // Auto-create the (user, today) row the first time the user lands here
  // without a session — same pattern as ChatPanel.
  useEffect(() => {
    if (sessionQuery.isPending) return;
    if (sessionQuery.data && !sessionQuery.data.session) {
      createOrResume.mutate();
    }
  }, [sessionQuery.isPending, sessionQuery.data, createOrResume]);

  const visibleMsgs = useMemo(() => visibleMessages(messages), [messages]);

  // Stop the call on unmount. (useVoice's own cleanup also fires; this
  // is defense-in-depth for tab swaps.)
  useEffect(() => {
    return () => {
      void voice.stop();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const [overwriteOpen, setOverwriteOpen] = useState(false);

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

  const statusLabel =
    voice.status === "idle"
      ? "Tap to start talking"
      : voice.status === "connecting"
        ? "Connecting…"
        : voice.status === "live"
          ? "Listening — say something"
          : "Ending…";

  const handleToggle = () => {
    if (voice.status === "idle") {
      void voice.start();
    } else {
      void voice.stop();
    }
  };

  const handleFinalize = (overwrite: boolean) => {
    if (!session) return;
    finalize.mutate({ sessionId: session.id, overwrite });
  };

  const hasUserTurns = visibleMsgs.some((m) => m.role === "user");
  const wrappedUp = session.phase === "wrapping_up";

  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardContent className="px-4 py-6 md:px-6">
          <div className="flex flex-col items-center gap-4">
            <button
              type="button"
              onClick={handleToggle}
              disabled={isEnding}
              className={cn(
                "relative flex h-24 w-24 items-center justify-center rounded-full",
                "border border-border/60 transition-all",
                isLive
                  ? "bg-destructive text-destructive-foreground shadow-lg ring-4 ring-destructive/20"
                  : "bg-accent text-accent-foreground hover:bg-accent/85",
                isConnecting && "animate-pulse",
                isEnding && "opacity-60",
              )}
              aria-label={isLive ? "Stop talking" : "Start talking"}
            >
              {isConnecting ? (
                <Loader2 className="h-10 w-10 animate-spin" aria-hidden />
              ) : isLive ? (
                <MicOff className="h-10 w-10" aria-hidden />
              ) : (
                <Mic className="h-10 w-10" aria-hidden />
              )}
            </button>
            <p className="text-sm text-muted-foreground" aria-live="polite">
              {statusLabel}
            </p>
            {voice.lastError ? (
              <p className="text-xs text-destructive">{voice.lastError}</p>
            ) : null}
          </div>
        </CardContent>
      </Card>

      {/* Transcript — same MessageList component the chat tab uses, so
       *  bubbles look identical between modes. */}
      {visibleMsgs.length > 0 ? (
        <Card>
          <CardContent className="px-4 py-4 md:px-6">
            <MessageList messages={visibleMsgs} partial="" />
          </CardContent>
        </Card>
      ) : null}

      {wrappedUp ? (
        <WrapUpAffordance
          onFinalize={() => handleFinalize(false)}
          pending={finalize.isPending}
        />
      ) : null}

      {/* Two-button finalize. Shown once the user has actually spoken;
       *  hidden during a live call so the user doesn't accidentally end
       *  while talking. */}
      {hasUserTurns && !isLive && !isConnecting ? (
        <Card>
          <CardContent className="flex flex-col gap-2 px-4 py-4 md:flex-row md:items-center md:justify-between md:gap-4 md:px-6">
            <p className="text-sm text-foreground/80">
              Done talking? Update today&apos;s check-in from this conversation.
            </p>
            <div className="flex flex-col gap-2 sm:flex-row">
              <Button
                variant="default"
                size="sm"
                onClick={() => handleFinalize(false)}
                disabled={finalize.isPending}
              >
                {finalize.isPending ? "Updating…" : "Finish — keep my edits"}
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setOverwriteOpen(true)}
                disabled={finalize.isPending}
              >
                Finish &amp; replace from chat
              </Button>
            </div>
          </CardContent>
        </Card>
      ) : null}

      {overwriteOpen ? (
        <Card className="border-destructive/40">
          <CardContent className="flex flex-col gap-3 px-4 py-4 md:px-6">
            <p className="text-sm text-foreground/85">
              Replace today&apos;s manual mood + notes with what was discussed?
              Slots the chat didn&apos;t cover stay as-is. Manual answers to
              specific questions are always overwritten by the chat — this
              toggle controls only your daily check-in fields.
            </p>
            <div className="flex flex-col gap-2 sm:flex-row sm:justify-end">
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setOverwriteOpen(false)}
                disabled={finalize.isPending}
              >
                Cancel
              </Button>
              <Button
                variant="destructive"
                size="sm"
                onClick={() => {
                  setOverwriteOpen(false);
                  handleFinalize(true);
                }}
                disabled={finalize.isPending}
              >
                Replace from chat
              </Button>
            </div>
          </CardContent>
        </Card>
      ) : null}
    </div>
  );
}
