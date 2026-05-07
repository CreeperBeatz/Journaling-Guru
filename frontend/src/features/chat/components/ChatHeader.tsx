import { Loader2, RefreshCw } from "lucide-react";

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Button, buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";

import type { ChatPhase } from "../api";

interface Props {
  phase: ChatPhase | undefined;
  hasUserTurns: boolean;
  hasMessages: boolean;
  finalizePending: boolean;
  resetPending: boolean;
  onFinalize: () => void;
  onReset: () => void;
}

// ChatHeader owns the two destructive-ish controls: "Update check-in"
// (extraction with overwrite warning) and "Reset" (wipes the
// conversation). Both are gated by AlertDialog so the user can't
// trigger either by accident.
//
// "Update check-in" is disabled until the user has actually said
// something — there's nothing to extract from an opener-only chat,
// and it'd be confusing to let the button fire.
//
// "Reset" is disabled when there are no messages at all (nothing to
// reset).
export function ChatHeader({
  phase,
  hasUserTurns,
  hasMessages,
  finalizePending,
  resetPending,
  onFinalize,
  onReset,
}: Props) {
  return (
    <div className="flex items-center justify-between gap-3 pb-2">
      <p className="text-xs uppercase tracking-wide text-muted-foreground">
        {phaseLabel(phase)}
      </p>
      <div className="flex items-center gap-2">
        <AlertDialog>
          <AlertDialogTrigger asChild>
            <Button
              size="sm"
              variant="ghost"
              disabled={!hasMessages || resetPending}
              aria-label="Reset chat"
              className="text-muted-foreground hover:text-foreground"
            >
              <RefreshCw className={cn("h-3.5 w-3.5", resetPending && "animate-spin")} />
            </Button>
          </AlertDialogTrigger>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Reset this conversation?</AlertDialogTitle>
              <AlertDialogDescription>
                This clears today&apos;s chat transcript and starts a fresh greeting.
                Your saved check-in (mood, emotions, notes, answers) <strong>stays put</strong> —
                only the conversation buffer is cleared. This can&apos;t be undone.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Keep chatting</AlertDialogCancel>
              <AlertDialogAction
                className={cn(buttonVariants({ variant: "destructive" }))}
                onClick={onReset}
              >
                Reset chat
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>

        <AlertDialog>
          <AlertDialogTrigger asChild>
            <Button
              size="sm"
              variant="default"
              disabled={!hasUserTurns || finalizePending}
            >
              {finalizePending ? (
                <>
                  <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
                  Updating…
                </>
              ) : (
                "Update check-in"
              )}
            </Button>
          </AlertDialogTrigger>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>Update your check-in from this chat?</AlertDialogTitle>
              <AlertDialogDescription>
                JournAI will read this conversation and overwrite today&apos;s mood,
                emotions, notes, and any answers it finds for your reflective questions.
                <br />
                <br />
                <strong>Anything you typed manually for those slots will be replaced</strong>
                with what was discussed here. Manual entries for slots the chat didn&apos;t cover
                are kept as-is. You can keep chatting after this — re-running &quot;Update check-in&quot;
                always re-extracts.
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>Cancel</AlertDialogCancel>
              <AlertDialogAction onClick={onFinalize}>Update check-in</AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </div>
    </div>
  );
}

// Phase labels are now strictly informational — none of them imply the
// chat is closed.
function phaseLabel(phase: ChatPhase | undefined): string {
  switch (phase) {
    case "greeting":
      return "Starting up";
    case "exploring":
      return "Reflecting";
    case "wrapping_up":
      return "Wrapping up";
    case "finalized":
      // Reachable only briefly mid-extraction. The extraction runs in
      // the background (status pill in the Today sticky bar). Once the
      // worker completes it rolls phase back to exploring.
      return "Reflecting";
    case "abandoned":
      return "Reflecting";
    default:
      return "";
  }
}
