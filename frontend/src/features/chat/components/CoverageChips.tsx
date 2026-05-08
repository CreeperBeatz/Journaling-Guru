import { motion, useReducedMotion } from "motion/react";

import { cn } from "@/lib/utils";
import { springTactile } from "@/lib/motion";
import type { Question } from "@/features/journal/api";

interface Props {
  questions: Question[];
  coveredIds: Set<string>;
}

// CoverageChips renders one pill per active question, lit when the
// post-turn classifier has marked that question_id covered during the
// session. Hidden when the user has no questions. Long lists scroll
// horizontally on narrow screens.
export function CoverageChips({ questions, coveredIds }: Props) {
  const reduced = useReducedMotion();
  if (questions.length === 0) return null;

  return (
    <div
      role="list"
      className="flex gap-2 overflow-x-auto pb-1 [-webkit-overflow-scrolling:touch] [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
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
              "shrink-0 rounded-full border px-3 py-1 text-xs leading-tight transition-colors",
              covered
                ? "border-accent/40 bg-accent/15 text-accent"
                : "border-border/60 bg-muted text-muted-foreground",
            )}
            aria-pressed={covered}
            title={q.prompt}
          >
            {truncated}
          </motion.span>
        );
      })}
    </div>
  );
}
