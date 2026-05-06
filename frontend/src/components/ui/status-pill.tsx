import { motion, useReducedMotion } from "motion/react";

import { cn } from "@/lib/utils";

export type StatusState = "idle" | "saving" | "saved" | "dirty" | "error";

interface Props {
  state: StatusState;
  message?: string;
  className?: string;
}

const dotTone: Record<StatusState, string> = {
  idle: "bg-muted-foreground/40",
  saving: "bg-warning",
  saved: "bg-success",
  dirty: "bg-warning",
  error: "bg-destructive",
};

const defaultLabel: Record<StatusState, string> = {
  idle: "Empty",
  saving: "Saving",
  saved: "Saved",
  dirty: "Unsaved",
  error: "Error",
};

// Layout-animated save indicator. Width FLIPs between states. Dot pulses
// when saving so the user gets an "in flight" cue without reading text.
export function StatusPill({ state, message, className }: Props) {
  const reduce = useReducedMotion();
  const text = message ?? defaultLabel[state];

  return (
    <motion.span
      layout={!reduce}
      transition={{ duration: 0.22 }}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border border-border bg-muted/60 px-2.5 py-0.5",
        "font-mono text-[11px] tabular-nums text-muted-foreground",
        className,
      )}
      role="status"
      aria-live="polite"
    >
      <motion.span
        className={cn("h-1.5 w-1.5 rounded-full", dotTone[state])}
        animate={
          state === "saving" && !reduce
            ? { scale: [1, 1.3, 1] }
            : { scale: 1 }
        }
        transition={
          state === "saving" && !reduce
            ? { duration: 0.8, repeat: Infinity, ease: "easeInOut" }
            : { duration: 0.15 }
        }
      />
      <motion.span layout={!reduce}>{text}</motion.span>
    </motion.span>
  );
}
