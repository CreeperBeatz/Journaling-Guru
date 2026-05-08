import { useMemo } from "react";

import type { MoodPoint } from "@/features/summaries/api";
import { cn } from "@/lib/utils";

export interface MoodLineProps {
  data: MoodPoint[];
  height?: number;
  className?: string;
}

/**
 * Small SVG line chart of mood (1-10) over time. One stroke from
 * --primary, dotted baseline at the user's median. Empty/single-point
 * series renders an empty-state hint. No axes labels — the shape is
 * the message.
 */
export function MoodLine({ data, height = 96, className }: MoodLineProps) {
  const ordered = useMemo(
    () => [...data].sort((a, b) => a.local_date.localeCompare(b.local_date)),
    [data],
  );

  if (ordered.length < 2) {
    return (
      <p className="text-sm text-muted-foreground">
        Need a few more days of mood to draw a line.
      </p>
    );
  }

  const w = 600; // viewBox; SVG scales to container width
  const h = height;
  const padX = 4;
  const padY = 8;

  const min = 1;
  const max = 10;
  const x = (i: number) =>
    padX + (i / (ordered.length - 1)) * (w - padX * 2);
  const y = (s: number) =>
    padY + (1 - (s - min) / (max - min)) * (h - padY * 2);

  const path = ordered
    .map((p, i) => `${i === 0 ? "M" : "L"} ${x(i).toFixed(1)} ${y(p.score).toFixed(1)}`)
    .join(" ");

  // Median baseline (rounded to 0.5 for visual cleanliness).
  const median = useMemo(() => {
    const sorted = ordered.map((p) => p.score).sort((a, b) => a - b);
    const mid = sorted.length >> 1;
    return sorted.length % 2
      ? sorted[mid]
      : (sorted[mid - 1] + sorted[mid]) / 2;
  }, [ordered]);
  const medianY = y(median);

  return (
    <svg
      viewBox={`0 0 ${w} ${h}`}
      preserveAspectRatio="none"
      className={cn("h-24 w-full", className)}
      aria-label={`Mood over ${ordered.length} days`}
    >
      {/* Median baseline */}
      <line
        x1={padX}
        x2={w - padX}
        y1={medianY}
        y2={medianY}
        stroke="hsl(var(--muted-foreground))"
        strokeWidth={1}
        strokeDasharray="3 4"
        opacity={0.5}
      />
      {/* Mood line */}
      <path
        d={path}
        fill="none"
        stroke="hsl(var(--primary))"
        strokeWidth={1.75}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      {/* Latest point — small accent dot so the eye lands on "now" */}
      <circle
        cx={x(ordered.length - 1)}
        cy={y(ordered[ordered.length - 1].score)}
        r={3}
        fill="hsl(var(--accent))"
      />
    </svg>
  );
}
