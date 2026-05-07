import { motion, useReducedMotion } from "motion/react";

import { cn } from "@/lib/utils";
import { springTactile } from "@/lib/motion";
import type { Question } from "@/features/journal/api";

interface Props {
  questions: Question[];
  coveredIds: Set<string>;
  /** Compact mode collapses each chip to a 10px dot (no text). Used by
   * the sticky condensed Today bar so coverage stays visible without
   * eating vertical space. Tooltips still expose the full prompt. */
  compact?: boolean;
}

// CoverageChips renders one pill (or dot, in compact mode) per active
// question, lit when the post-turn classifier has marked that
// question_id covered during the session.
//
// Deliberately collapsible — we hide entirely when the user has no
// questions. Long lists scroll horizontally on narrow screens.
export function CoverageChips({ questions, coveredIds, compact = false }: Props) {
  const reduced = useReducedMotion();
  if (questions.length === 0) return null;

  return (
    <div
      role="list"
      className={cn(
        "flex overflow-x-auto [-webkit-overflow-scrolling:touch] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden",
        compact ? "items-center gap-1.5" : "gap-2 pb-1",
      )}
      aria-label="Question coverage"
    >
      {questions.map((q) => {
        const covered = coveredIds.has(q.id);
        const truncated = q.prompt.length > 32 ? q.prompt.slice(0, 32) + "…" : q.prompt;
        return (
          <motion.span
            key={q.id}
            role="listitem"
            layout={!reduced}
            transition={reduced ? undefined : springTactile}
            className={cn(
              "shrink-0 transition-colors",
              compact
                ? cn(
                    "h-2 w-2 rounded-full",
                    covered ? "bg-accent" : "bg-border",
                  )
                : cn(
                    "rounded-full border px-3 py-1 text-xs leading-tight",
                    covered
                      ? "border-accent/40 bg-accent/15 text-accent"
                      : "border-border/60 bg-muted text-muted-foreground",
                  ),
            )}
            aria-pressed={covered}
            aria-label={compact ? `${q.prompt}${covered ? " (covered)" : ""}` : undefined}
            title={q.prompt}
          >
            {compact ? null : truncated}
          </motion.span>
        );
      })}
    </div>
  );
}
