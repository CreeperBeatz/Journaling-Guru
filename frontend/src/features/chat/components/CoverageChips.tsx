import { motion, useReducedMotion } from "motion/react";

import { cn } from "@/lib/utils";
import { springTactile } from "@/lib/motion";

interface Props {
  // Topic codes covered so far. Source: chat_sessions.covered_question_ids
  // (column name retained from the per-question era; under the Energy
  // Audit pivot it stores topic codes — see backend/internal/llm/chat/
  // coverage.go).
  coveredCodes: Set<string>;
}

// Fixed Energy Audit topics, in the spec's prompt order.
//
// `code` matches the backend coverage classifier's vocabulary
// (drained / charged / grateful / else); `label` is what the user
// sees on the chip.
const TOPICS: Array<{ code: string; label: string }> = [
  { code: "drained", label: "Drained" },
  { code: "charged", label: "Charged" },
  { code: "grateful", label: "Grateful" },
  { code: "else", label: "Anything else" },
];

// CoverageChips renders four pills — one per fixed Energy Audit topic.
// A pill lights up when the post-turn classifier has marked that topic
// substantively covered in the session transcript.
export function CoverageChips({ coveredCodes }: Props) {
  const reduced = useReducedMotion();

  return (
    <div
      role="list"
      aria-label="Topic coverage"
      className="flex flex-wrap gap-1.5"
    >
      {TOPICS.map((t) => {
        const covered = coveredCodes.has(t.code);
        return (
          <motion.span
            key={t.code}
            role="listitem"
            layout={!reduced}
            transition={reduced ? undefined : springTactile}
            className={cn(
              "shrink-0 rounded-full border px-2.5 py-0.5 text-[11px] leading-tight transition-colors",
              covered
                ? "border-accent/40 bg-accent/15 text-accent"
                : "border-border/60 bg-muted text-muted-foreground",
            )}
            aria-pressed={covered}
          >
            {t.label}
          </motion.span>
        );
      })}
    </div>
  );
}
