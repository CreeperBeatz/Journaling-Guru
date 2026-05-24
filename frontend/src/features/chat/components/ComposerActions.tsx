import { useEffect, useRef, useState } from "react";
import { Loader2, MoreVertical, Sparkles, RefreshCw, CheckCircle2, Undo2 } from "lucide-react";

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

interface WrapUpButtonProps {
  pending: boolean;
  disabled: boolean;
  // wrappedUp = the session has already entered the wrapping_up phase
  // (the model proposed wrap-up or the user already triggered it).
  // Renders a non-interactive "Wrapping up" pill so the user sees
  // they're in the closing pass without losing the affordance.
  wrappedUp: boolean;
  onWrapUp: () => void;
}

// WrapUpButton — small pill CTA shown next to the Send button. Lets the
// user signal "I'm ready to wrap up" before all topics are covered.
export function WrapUpButton({ pending, disabled, wrappedUp, onWrapUp }: WrapUpButtonProps) {
  const inert = pending || disabled || wrappedUp;
  let label = "Wrap up";
  if (pending) label = "Wrapping…";
  else if (wrappedUp) label = "Wrapping up";
  return (
    <button
      type="button"
      onClick={wrappedUp ? undefined : onWrapUp}
      disabled={inert}
      aria-label="Wrap up the conversation"
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs font-medium transition-colors",
        wrappedUp
          ? "border-border bg-muted text-muted-foreground"
          : "border-accent/40 bg-accent/10 text-accent hover:bg-accent/20",
        "disabled:opacity-60",
      )}
    >
      {pending ? (
        <Loader2 className="h-3 w-3 animate-spin" />
      ) : (
        <Sparkles className="h-3 w-3" />
      )}
      {label}
    </button>
  );
}

interface KebabProps {
  finishDisabled: boolean;
  restartDisabled: boolean;
  finishPending: boolean;
  restartPending: boolean;
  onFinish: () => void;
  onRestart: () => void;
  // wrappedUp toggles the "Cancel wrap-up" menu item. When the session
  // is in wrapping_up, surface a one-click escape hatch that flips back
  // to exploring without sending a message. Hidden otherwise.
  wrappedUp: boolean;
  cancelWrapUpPending: boolean;
  onCancelWrapUp: () => void;
}

// ChatKebab — the (⋮) menu with "Cancel wrap-up" (conditional),
// "Finish check-in", and "Restart conversation". Finish/Restart go
// through AlertDialog (state-changing / destructive); cancel-wrap-up
// is single-click since it's trivially reversible.
export function ChatKebab({
  finishDisabled,
  restartDisabled,
  finishPending,
  restartPending,
  onFinish,
  onRestart,
  wrappedUp,
  cancelWrapUpPending,
  onCancelWrapUp,
}: KebabProps) {
  const [menuOpen, setMenuOpen] = useState(false);
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
              "absolute bottom-full left-0 z-30 mb-2 min-w-[200px] rounded-md border bg-popover p-1 shadow-md",
            )}
          >
            {wrappedUp ? (
              <MenuItem
                icon={
                  cancelWrapUpPending ? (
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <Undo2 className="h-3.5 w-3.5" />
                  )
                }
                label={cancelWrapUpPending ? "Cancelling…" : "Cancel wrap-up"}
                disabled={cancelWrapUpPending}
                onClick={() => {
                  setMenuOpen(false);
                  onCancelWrapUp();
                }}
              />
            ) : null}
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
                onFinish();
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
