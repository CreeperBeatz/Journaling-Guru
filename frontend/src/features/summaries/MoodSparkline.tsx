import { useMemo } from "react";

import { cn } from "@/lib/utils";

import type { MoodPoint } from "./api";

interface Props {
  data: MoodPoint[];
  className?: string;
  // Visual height — width is responsive via SVG viewBox.
  height?: number;
}

// MoodSparkline renders a tiny SVG line chart of daily mood scores. We
// handroll instead of pulling in recharts so the bundle stays small;
// summaries are infrequent enough that the chart only needs to look
// good once on a static dataset.
//
// X is uniformly spaced by index (not date) — gaps in journaling become
// shorter line segments rather than ugly horizontal stretches. The
// score-axis is fixed [1,10] so two months are visually comparable.
export function MoodSparkline({ data, className, height = 64 }: Props) {
  const path = useMemo(() => buildPath(data), [data]);
  if (data.length === 0) {
    return (
      <div
        className={cn(
          "flex items-center justify-center rounded-md border border-dashed border-border bg-muted/30 px-4 text-xs text-muted-foreground",
          className,
        )}
        style={{ height }}
      >
        No mood data yet — write a few daily entries to see your trend here.
      </div>
    );
  }
  // viewBox: 0..100 wide, 0..10 tall (inverted so 10=top).
  return (
    <svg
      role="img"
      aria-label={`Mood over the last ${data.length} day${data.length === 1 ? "" : "s"}`}
      className={cn("w-full", className)}
      style={{ height }}
      viewBox="0 0 100 10"
      preserveAspectRatio="none"
    >
      {/* Faint mid-line at score=5 to anchor the scale visually. */}
      <line
        x1={0}
        y1={5}
        x2={100}
        y2={5}
        stroke="currentColor"
        strokeOpacity={0.12}
        strokeWidth={0.05}
        vectorEffect="non-scaling-stroke"
      />
      <path
        d={path}
        fill="none"
        stroke="currentColor"
        strokeWidth={1.5}
        strokeLinecap="round"
        strokeLinejoin="round"
        vectorEffect="non-scaling-stroke"
        className="text-accent"
      />
      {data.map((p, i) => {
        const x = data.length === 1 ? 50 : (i / (data.length - 1)) * 100;
        const y = 10 - clamp(p.score, 1, 10);
        return (
          <circle
            key={p.local_date}
            cx={x}
            cy={y}
            r={1}
            vectorEffect="non-scaling-stroke"
            className="fill-accent"
          />
        );
      })}
    </svg>
  );
}

function buildPath(data: MoodPoint[]): string {
  if (data.length === 0) return "";
  if (data.length === 1) {
    // Single point: a horizontal stub so the path has something to draw.
    const y = 10 - clamp(data[0].score, 1, 10);
    return `M 0 ${y} L 100 ${y}`;
  }
  return data
    .map((p, i) => {
      const x = (i / (data.length - 1)) * 100;
      const y = 10 - clamp(p.score, 1, 10);
      return `${i === 0 ? "M" : "L"} ${x.toFixed(2)} ${y.toFixed(2)}`;
    })
    .join(" ");
}

function clamp(v: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, v));
}
