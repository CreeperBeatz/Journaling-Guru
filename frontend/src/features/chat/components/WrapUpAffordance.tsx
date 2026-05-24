import { Loader2 } from "lucide-react";
import { motion, useReducedMotion } from "motion/react";

import { Button } from "@/components/ui/button";
import { easeStandard } from "@/lib/motion";

import type { ExtractionStatus } from "../api";

interface Props {
  onFinalize: () => void;
  // pending mirrors finalize.isPending — the brief window between click
  // and the server's optimistic extraction_status flip.
  pending: boolean;
  extractionStatus: ExtractionStatus;
  extractionError: string | null;
  onRetry: () => void;
}

// WrapUpAffordance surfaces a soft "Update check-in?" nudge when the
// model has called propose_wrap_up. It's optional — the user can
// ignore it and keep typing; the next user turn flips the phase back
// to exploring and the affordance disappears. Clicking the action
// finalizes immediately (no confirm) and the card itself flips to a
// loader / retry view while extraction runs.
export function WrapUpAffordance({
  onFinalize,
  pending,
  extractionStatus,
  extractionError,
  onRetry,
}: Props) {
  const reduced = useReducedMotion();
  const inFlight =
    pending || extractionStatus === "pending" || extractionStatus === "running";
  const failed = !inFlight && extractionStatus === "failed";

  return (
    <motion.div
      initial={reduced ? false : { opacity: 0, y: 4 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.22, ease: easeStandard }}
      className="rounded-lg border border-accent/30 bg-accent/5 px-4 py-3 text-sm text-accent-foreground"
    >
      {inFlight ? (
        <div
          role="status"
          aria-live="polite"
          className="flex items-center gap-2 text-accent"
        >
          <Loader2 className="h-4 w-4 animate-spin" aria-hidden />
          <span className="font-medium">Updating check-in…</span>
        </div>
      ) : failed ? (
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <p className="text-destructive">
            {extractionError ?? "Couldn't update the check-in."}
          </p>
          <Button
            variant="outline"
            size="sm"
            onClick={onRetry}
            className="self-start sm:self-auto"
          >
            Retry
          </Button>
        </div>
      ) : (
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <p className="text-foreground/85">
            Sounds like a good place to pause. Want to refresh today&apos;s check-in with what we&apos;ve covered?
          </p>
          <Button
            variant="default"
            size="sm"
            onClick={onFinalize}
            className="self-start sm:self-auto"
          >
            Update check-in
          </Button>
        </div>
      )}
    </motion.div>
  );
}
