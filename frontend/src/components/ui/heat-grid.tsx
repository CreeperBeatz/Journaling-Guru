import { useMemo } from "react";
import { motion, useReducedMotion } from "motion/react";

import { cn } from "@/lib/utils";

export type HeatLevel = 0 | 1 | 2 | 3 | 4;

export interface HeatCellData {
  date: string; // YYYY-MM-DD
  level: HeatLevel;
  moodUp: boolean;
}

export type HeatView = "year" | "month" | "week";

export interface HeatGridProps {
  cells: HeatCellData[];
  view?: HeatView;
  /** Anchor date for the grid (default: today). Year view shows the 52
   *  weeks before this date plus the current week; month view shows the
   *  calendar month containing this date; week view shows the week
   *  containing this date. */
  anchor?: string;
  onSelect?: (date: string) => void;
  className?: string;
}

const LEVEL_VAR: Record<HeatLevel, string> = {
  0: "var(--heat-empty)",
  1: "var(--heat-l1)",
  2: "var(--heat-l2)",
  3: "var(--heat-l3)",
  4: "var(--heat-l4)",
};

function parseISO(s: string): Date {
  const [y, m, d] = s.split("-").map(Number);
  return new Date(Date.UTC(y, m - 1, d));
}

function formatISO(d: Date): string {
  const y = d.getUTCFullYear();
  const m = String(d.getUTCMonth() + 1).padStart(2, "0");
  const dd = String(d.getUTCDate()).padStart(2, "0");
  return `${y}-${m}-${dd}`;
}

function addDays(d: Date, n: number): Date {
  const x = new Date(d);
  x.setUTCDate(x.getUTCDate() + n);
  return x;
}

function startOfWeek(d: Date): Date {
  // Sunday-start week. Matches GitHub-style heatmaps.
  const dow = d.getUTCDay();
  return addDays(d, -dow);
}

function startOfMonth(d: Date): Date {
  return new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), 1));
}

function endOfMonth(d: Date): Date {
  return new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + 1, 0));
}

interface ResolvedRange {
  start: Date;
  end: Date;
  cols: number;
  rows: number;
}

function resolveRange(view: HeatView, anchor: Date): ResolvedRange {
  if (view === "week") {
    const s = startOfWeek(anchor);
    return { start: s, end: addDays(s, 6), cols: 1, rows: 7 };
  }
  if (view === "month") {
    const monthStart = startOfMonth(anchor);
    const monthEnd = endOfMonth(anchor);
    const gridStart = startOfWeek(monthStart);
    const gridEnd = addDays(startOfWeek(monthEnd), 6);
    const totalDays = Math.round(
      (gridEnd.getTime() - gridStart.getTime()) / 86_400_000,
    ) + 1;
    return { start: gridStart, end: gridEnd, cols: totalDays / 7, rows: 7 };
  }
  // year: 53 columns × 7 rows ending at the week containing `anchor`.
  const endWeekStart = startOfWeek(anchor);
  const start = addDays(endWeekStart, -52 * 7);
  const end = addDays(endWeekStart, 6);
  return { start, end, cols: 53, rows: 7 };
}

export function HeatGrid({
  cells,
  view = "year",
  anchor,
  onSelect,
  className,
}: HeatGridProps) {
  const reduceMotion = useReducedMotion();

  const anchorDate = useMemo(
    () => (anchor ? parseISO(anchor) : new Date()),
    [anchor],
  );
  const range = useMemo(() => resolveRange(view, anchorDate), [view, anchorDate]);
  const todayISO = useMemo(() => formatISO(new Date()), []);

  const byDate = useMemo(() => {
    const m = new Map<string, HeatCellData>();
    for (const c of cells) m.set(c.date, c);
    return m;
  }, [cells]);

  // Build column-major: each column = one week (7 days), top row = Sunday.
  const columns: { weekIndex: number; days: { iso: string; data?: HeatCellData; outOfRange: boolean; isToday: boolean }[] }[] = [];
  for (let col = 0; col < range.cols; col++) {
    const days = [];
    for (let row = 0; row < 7; row++) {
      const d = addDays(range.start, col * 7 + row);
      const iso = formatISO(d);
      const inRange = d >= range.start && d <= range.end;
      const future = d.getTime() > parseISO(todayISO).getTime();
      days.push({
        iso,
        data: byDate.get(iso),
        outOfRange: !inRange || future,
        isToday: iso === todayISO,
      });
    }
    columns.push({ weekIndex: col, days });
  }

  // Cell sizing per view — kept in tailwind classes so the grid scales
  // crisply on retina without manual pixel math.
  const cellSize =
    view === "week"
      ? "h-12 w-12 rounded-md"
      : view === "month"
        ? "h-8 w-8 rounded-sm"
        : "h-3 w-3 rounded-[2px]";
  const gapClass = view === "week" || view === "month" ? "gap-1" : "gap-[2px]";

  return (
    <div
      className={cn(
        "inline-flex flex-col",
        className,
      )}
      role="grid"
      aria-label="Journaling activity heatmap"
    >
      <div className={cn("flex", gapClass)}>
        {columns.map((col) => (
          <div key={col.weekIndex} className={cn("flex flex-col", gapClass)}>
            {col.days.map((day) => {
              const level = day.data?.level ?? 0;
              const moodUp = day.data?.moodUp ?? false;
              const interactive = !day.outOfRange && !!onSelect;
              return (
                <motion.button
                  key={day.iso}
                  type="button"
                  role="gridcell"
                  disabled={!interactive}
                  onClick={() => interactive && onSelect?.(day.iso)}
                  whileHover={
                    interactive && !reduceMotion ? { scale: 1.18 } : undefined
                  }
                  transition={{ type: "spring", stiffness: 400, damping: 22 }}
                  className={cn(
                    cellSize,
                    "relative outline-none transition-colors",
                    "focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 ring-offset-background",
                    !interactive && "cursor-default",
                    day.outOfRange && "opacity-30",
                    day.isToday && "ring-1 ring-foreground/40",
                  )}
                  style={{
                    backgroundColor: LEVEL_VAR[level],
                    boxShadow: moodUp
                      ? "inset 0 0 0 1px var(--heat-mood)"
                      : undefined,
                  }}
                  aria-label={cellLabel(day.iso, day.data)}
                  title={cellLabel(day.iso, day.data)}
                />
              );
            })}
          </div>
        ))}
      </div>
    </div>
  );
}

function cellLabel(iso: string, data?: HeatCellData): string {
  if (!data || data.level === 0) return `${iso}: nothing yet`;
  const parts = [iso, levelLabel(data.level)];
  if (data.moodUp) parts.push("mood up");
  return parts.join(" · ");
}

function levelLabel(level: HeatLevel): string {
  switch (level) {
    case 1:
      return "short";
    case 2:
      return "mid";
    case 3:
      return "deep";
    case 4:
      return "deep streak";
    default:
      return "empty";
  }
}

/**
 * Compute level + moodUp for a single day's stats. Mirrors the rule in
 * DESIGN.md → History so we can rebuild the heat ramp client-side from
 * the heatmap endpoint without a second round-trip.
 *
 * Pass `prevLevels` as the chronological run of levels up to this day
 * (excluding it) — used to detect "deep streak" (3+ consecutive level-3
 * days).
 */
export function deriveHeatLevel(
  answered: number,
  chatTurns: number,
  prevLevel3Run: number,
): HeatLevel {
  if (answered === 0 && chatTurns === 0) return 0;
  if (answered <= 2 && chatTurns < 3) return 1;
  if (answered >= 6 && chatTurns >= 3) {
    return prevLevel3Run >= 2 ? 4 : 3;
  }
  if (answered >= 3 || chatTurns >= 3) return 2;
  return 1;
}

/**
 * Build a chronological array of <HeatCellData> from a raw heatmap
 * response. Computes `level` from answered/chat_turns/prev streak and
 * `moodUp` from mood_score >= 7 (top tertile of the 1-10 scale).
 */
export function buildHeatCells(
  days: { local_date: string; answered: number; chat_turns: number; mood?: number | null }[],
): HeatCellData[] {
  const sorted = [...days].sort((a, b) => a.local_date.localeCompare(b.local_date));
  const out: HeatCellData[] = [];
  let level3Run = 0;
  for (const d of sorted) {
    const level = deriveHeatLevel(d.answered, d.chat_turns, level3Run);
    if (level >= 3) level3Run += 1;
    else level3Run = 0;
    const moodUp = (d.mood ?? 0) >= 7;
    out.push({ date: d.local_date, level, moodUp });
  }
  return out;
}
