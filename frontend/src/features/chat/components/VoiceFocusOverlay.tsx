import { Loader2, Mic, MicOff } from "lucide-react";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

import type { VoiceStatus } from "../voice";

interface Props {
  status: VoiceStatus;
  lastError: string | null;
  onToggle: () => void;
  onDone: () => void;
}

// Full-bleed focus surface that takes over the chat view while a voice
// session is connecting / live / ending. Transcripts persist underneath
// (appendVoiceTranscript writes to chat_messages live) and are revealed
// when the overlay unmounts on return-to-idle.
export function VoiceFocusOverlay({ status, lastError, onToggle, onDone }: Props) {
  const isLive = status === "live";
  const isConnecting = status === "connecting";
  const isEnding = status === "ending";

  const statusLabel =
    status === "connecting"
      ? "Connecting…"
      : status === "live"
        ? "Listening — speak whenever"
        : status === "ending"
          ? "Ending…"
          : "Tap to start talking";

  return (
    <div
      className="fixed inset-x-0 bottom-0 z-20 flex flex-col items-center justify-center bg-background md:left-60"
      style={{
        // Same offset pattern ChatPanel uses: AppShell mobile header
        // height plus DailyEntry's sticky tab strip (~3.5rem).
        top: "calc(var(--app-mobile-header-h, 0px) + 3.5rem)",
      }}
      role="dialog"
      aria-label="Voice session"
    >
      <div className="flex flex-col items-center gap-6 px-6">
        <button
          type="button"
          onClick={onToggle}
          disabled={isEnding}
          className={cn(
            "relative flex h-32 w-32 items-center justify-center rounded-full",
            "border border-border/60 transition-all",
            isLive
              ? "bg-destructive text-destructive-foreground shadow-lg ring-4 ring-destructive/20 animate-pulse"
              : "bg-accent text-accent-foreground hover:bg-accent/85",
            isConnecting && "animate-pulse",
            isEnding && "opacity-60",
          )}
          aria-label={isLive ? "Stop talking" : "Start talking"}
        >
          {isConnecting ? (
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
        {lastError ? <p className="text-xs text-destructive">{lastError}</p> : null}
        <Button variant="outline" size="sm" onClick={onDone} disabled={isEnding}>
          Done
        </Button>
        <div className="mt-2 flex max-w-sm flex-col gap-1.5 text-center text-xs text-muted-foreground/80">
          <p>
            Tell the assistant to wrap up when you want to finish your daily
            reflection.
          </p>
          <p>Close the call whenever you feel ready.</p>
        </div>
      </div>
    </div>
  );
}
