import { useMemo } from "react";

import type { EmotionCount } from "@/features/summaries/api";
import { cn } from "@/lib/utils";

export interface WordCloudProps {
  /** Frequency-weighted words. Sourced from stats.emotions today; will
   *  fold in summary topics once the backend exposes them. */
  words: EmotionCount[];
  /** Optional accent pick — the LLM's "noticed" word. When omitted,
   *  the most-frequent word is highlighted. */
  noticed?: string;
  className?: string;
}

/**
 * Lightweight client-only word cloud. Sizes by frequency on a 4-step
 * scale; the accent pick (the LLM's "noticed" word, or the most
 * frequent if no pick) gets `text-accent`. Other words taper from
 * `text-foreground` to `text-foreground/70` by frequency tier.
 *
 * Deliberately no external lib — keeps the markdown chunk lean.
 */
export function WordCloud({ words, noticed, className }: WordCloudProps) {
  const sorted = useMemo(
    () => [...words].sort((a, b) => b.count - a.count).slice(0, 24),
    [words],
  );
  const accentPick = useMemo(() => {
    if (noticed) return noticed.toLowerCase();
    return sorted[0]?.emotion?.toLowerCase() ?? null;
  }, [noticed, sorted]);

  if (sorted.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        Words from your reflections will surface here as they accrue.
      </p>
    );
  }

  const max = sorted[0].count;
  // 4-step size scale: lg / md / sm / xs by frequency tier.
  const sizeFor = (count: number): string => {
    const r = count / max;
    if (r > 0.75) return "text-2xl font-serif";
    if (r > 0.5) return "text-xl";
    if (r > 0.25) return "text-base";
    return "text-sm";
  };

  return (
    <ul
      className={cn(
        "flex flex-wrap items-baseline justify-center gap-x-3 gap-y-1.5 leading-snug",
        className,
      )}
    >
      {sorted.map((w) => {
        const isAccent = w.emotion.toLowerCase() === accentPick;
        return (
          <li
            key={w.emotion}
            className={cn(
              sizeFor(w.count),
              isAccent
                ? "text-accent font-medium"
                : "text-foreground/80",
              "capitalize",
            )}
            title={`${w.emotion} · ${w.count}`}
          >
            {w.emotion}
          </li>
        );
      })}
    </ul>
  );
}
