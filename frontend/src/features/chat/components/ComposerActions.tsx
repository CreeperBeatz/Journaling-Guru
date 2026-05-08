import { useEffect, useRef, useState } from "react";
import { Loader2, MoreVertical, Sparkles, RefreshCw, CheckCircle2 } from "lucide-react";

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";

interface Props {
  // Visibility / state for the wrap-up button. Hidden when there are
  // no remaining topics (or no user turns yet).
  showWrapUp: boolean;
  // True while a wrap-up SSE stream is in flight; the button shows a
  // spinner and disables itself.
  wrapUpPending: boolean;
  // True while the kebab actions are blocked (e.g. mid-stream).
  busy: boolean;
  // True when "Finish check-in" should be disabled — the chat hasn't
  // had any user turns yet, so there's nothing to extract.
  finishDisabled: boolean;
  // True when "Restart" should be disabled — no messages at all.
  restartDisabled: boolean;
  // Pending flags for the kebab actions.
  finishPending: boolean;
  restartPending: boolean;

  onWrapUp: () => void;
  onFinish: () => void;
  onRestart: () => void;
}

// ComposerActions is the compact action row that lives next to the
// composer textarea: an optional "Wrap up" pill (when topics remain
// uncovered) and a kebab (⋮) menu with "Finish check-in" and "Restart
// conversation" items.
//
// Both kebab actions go through AlertDialog because they're either
// state-changing (extraction) or destructive (restart) and we want a
// confirmation step.
export function ComposerActions({
  showWrapUp,
  wrapUpPending,
  busy,
  finishDisabled,
  restartDisabled,
  finishPending,
  restartPending,
  onWrapUp,
  onFinish,
  onRestart,
}: Props) {
  const [menuOpen, setMenuOpen] = useState(false);
  const [confirmFinish, setConfirmFinish] = useState(false);
  const [confirmRestart, setConfirmRestart] = useState(false);
  const menuRef = useRef<HTMLDivElement | null>(null);

  // Close the kebab on outside click / Esc.
  useEffect(() => {
    if (!menuOpen) return;
    const handler = (e: MouseEvent) => {
      if (!menuRef.current?.contains(e.target as Node)) {
        setMenuOpen(false);
      }
    };
    const escHandler = (e: KeyboardEvent) => {
      if (e.key === "Escape") setMenuOpen(false);
    };
    document.addEventListener("mousedown", handler);
    document.addEventListener("keydown", escHandler);
    return () => {
      document.removeEventListener("mousedown", handler);
      document.removeEventListener("keydown", escHandler);
    };
  }, [menuOpen]);

  return (
    <>
      <div className="flex items-center gap-2">
        {showWrapUp ? (
          <button
            type="button"
            onClick={onWrapUp}
            disabled={wrapUpPending || busy}
            aria-label="Wrap up the conversation"
            className={cn(
              "inline-flex items-center gap-1.5 rounded-full border border-accent/40 bg-accent/10",
              "px-3 py-1 text-xs font-medium text-accent transition-colors",
              "hover:bg-accent/20 disabled:opacity-60",
            )}
          >
            {wrapUpPending ? (
              <Loader2 className="h-3 w-3 animate-spin" />
            ) : (
              <Sparkles className="h-3 w-3" />
            )}
            {wrapUpPending ? "Wrapping…" : "Wrap up"}
          </button>
        ) : null}

        <div ref={menuRef} className="relative">
          <button
            type="button"
            onClick={() => setMenuOpen((v) => !v)}
            aria-label="More actions"
            aria-expanded={menuOpen}
            className={cn(
              "flex h-8 w-8 items-center justify-center rounded-full",
              "text-muted-foreground transition-colors",
              "hover:bg-secondary hover:text-foreground",
            )}
          >
            <MoreVertical className="h-4 w-4" />
          </button>
          {menuOpen ? (
            <div
              role="menu"
              className={cn(
                "absolute bottom-full right-0 z-30 mb-2 min-w-[200px] rounded-md border bg-popover p-1 shadow-md",
              )}
            >
              <MenuItem
                icon={
                  finishPending ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <CheckCircle2 className="h-3.5 w-3.5" />
                  )
                }
                label={finishPending ? "Updating…" : "Finish check-in"}
                disabled={finishDisabled || finishPending}
                onClick={() => {
                  setMenuOpen(false);
                  setConfirmFinish(true);
                }}
              />
              <MenuItem
                icon={
                  restartPending ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <RefreshCw className="h-3.5 w-3.5" />
                  )
                }
                label={restartPending ? "Restarting…" : "Restart conversation"}
                disabled={restartDisabled || restartPending}
                destructive
                onClick={() => {
                  setMenuOpen(false);
                  setConfirmRestart(true);
                }}
              />
            </div>
          ) : null}
        </div>
      </div>

      <AlertDialog open={confirmFinish} onOpenChange={setConfirmFinish}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Update your check-in from this chat?</AlertDialogTitle>
            <AlertDialogDescription>
              JournAI will read this conversation and fill in today&apos;s mood,
              drainers, chargers, gratitude, and reflection from what was
              discussed here.
              <br />
              <br />
              <strong>Manual edits you&apos;ve already made survive</strong> —
              extraction only fills empty slots. You can keep chatting after
              this; re-running &quot;Finish check-in&quot; always re-extracts.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={onFinish}>
              Finish check-in
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={confirmRestart} onOpenChange={setConfirmRestart}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Restart this conversation?</AlertDialogTitle>
            <AlertDialogDescription>
              This clears today&apos;s chat transcript and starts a fresh
              greeting. Your saved check-in (mood, drainers, chargers, etc.)
              <strong> stays put</strong> — only the conversation buffer is
              cleared. This can&apos;t be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Keep chatting</AlertDialogCancel>
            <AlertDialogAction
              className={cn(buttonVariants({ variant: "destructive" }))}
              onClick={onRestart}
            >
              Restart
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}

function MenuItem({
  icon,
  label,
  onClick,
  disabled,
  destructive,
}: {
  icon: React.ReactNode;
  label: string;
  onClick: () => void;
  disabled?: boolean;
  destructive?: boolean;
}) {
  return (
    <button
      type="button"
      role="menuitem"
      onClick={onClick}
      disabled={disabled}
      className={cn(
        "flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs",
        "hover:bg-secondary disabled:opacity-50",
        destructive ? "text-destructive" : "text-foreground",
      )}
    >
      {icon}
      <span>{label}</span>
    </button>
  );
}
