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
  /** Anchor date (default: today). Year view shows the 12 months
   *  ending at the anchor's month; month view shows the anchor's
   *  month; week view shows the week containing the anchor. */
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

function startOfMonth(year: number, month: number): Date {
  return new Date(Date.UTC(year, month, 1));
}

function daysInMonth(year: number, month: number): number {
  return new Date(Date.UTC(year, month + 1, 0)).getUTCDate();
}

function startOfWeek(d: Date): Date {
  const dow = d.getUTCDay();
  const x = new Date(d);
  x.setUTCDate(x.getUTCDate() - dow);
  return x;
}

const WEEKDAYS = ["S", "M", "T", "W", "T", "F", "S"];

export function HeatGrid({
  cells,
  view = "year",
  anchor,
  onSelect,
  className,
}: HeatGridProps) {
  const anchorDate = useMemo(
    () => (anchor ? parseISO(anchor) : new Date()),
    [anchor],
  );
  const todayISO = useMemo(() => formatISO(new Date()), []);

  const byDate = useMemo(() => {
    const m = new Map<string, HeatCellData>();
    for (const c of cells) m.set(c.date, c);
    return m;
  }, [cells]);

  if (view === "week") {
    const start = startOfWeek(anchorDate);
    return (
      <WeekRow
        start={start}
        byDate={byDate}
        todayISO={todayISO}
        onSelect={onSelect}
        className={className}
      />
    );
  }

  if (view === "month") {
    return (
      <div className={cn("flex justify-center", className)}>
        <MonthGrid
          year={anchorDate.getUTCFullYear()}
          month={anchorDate.getUTCMonth()}
          byDate={byDate}
          todayISO={todayISO}
          onSelect={onSelect}
          size="lg"
          showWeekdays
        />
      </div>
    );
  }

  // Year view: 12 months ending at the anchor's month, laid out in a
  // responsive grid. No horizontal scroll — cards wrap to the next row.
  const months: { year: number; month: number }[] = [];
  for (let i = 11; i >= 0; i--) {
    const d = new Date(
      Date.UTC(anchorDate.getUTCFullYear(), anchorDate.getUTCMonth() - i, 1),
    );
    months.push({ year: d.getUTCFullYear(), month: d.getUTCMonth() });
  }

  return (
    <div
      className={cn(
        "grid w-full gap-x-4 gap-y-6",
        "grid-cols-2 sm:grid-cols-3 md:grid-cols-4",
        className,
      )}
    >
      {months.map(({ year, month }) => (
        <MonthGrid
          key={`${year}-${month}`}
          year={year}
          month={month}
          byDate={byDate}
          todayISO={todayISO}
          onSelect={onSelect}
          size="sm"
        />
      ))}
    </div>
  );
}

interface MonthGridProps {
  year: number;
  month: number; // 0-indexed
  byDate: Map<string, HeatCellData>;
  todayISO: string;
  onSelect?: (date: string) => void;
  size: "sm" | "lg";
  showWeekdays?: boolean;
}

function MonthGrid({
  year,
  month,
  byDate,
  todayISO,
  onSelect,
  size,
  showWeekdays,
}: MonthGridProps) {
  const reduceMotion = useReducedMotion();
  const monthLabel = startOfMonth(year, month).toLocaleDateString(undefined, {
    month: "short",
    year: "numeric",
    timeZone: "UTC",
  });

  const totalDays = daysInMonth(year, month);
  const firstDow = startOfMonth(year, month).getUTCDay(); // 0=Sun
  const cellSize = size === "lg" ? "h-10 w-10 rounded-md" : "h-7 w-7 rounded-md";
  const gapClass = size === "lg" ? "gap-1.5" : "gap-1";
  const labelClass =
    size === "lg"
      ? "text-sm font-medium"
      : "text-xs font-medium";

  // Pad with leading empties so the 1st lands on its real weekday.
  const leadingEmpties = Array.from({ length: firstDow }, (_, i) => `e-${i}`);
  const dayNumbers = Array.from({ length: totalDays }, (_, i) => i + 1);

  return (
    <div className="flex flex-col items-center">
      <p className={cn("mb-2 self-start", labelClass)}>{monthLabel}</p>
      {showWeekdays ? (
        <div className={cn("mb-1 grid grid-cols-7", gapClass)}>
          {WEEKDAYS.map((w, i) => (
            <span
              key={`${w}-${i}`}
              className={cn(
                "text-center font-mono text-[10px] uppercase tracking-wider text-muted-foreground",
                cellSize,
                "flex items-center justify-center",
              )}
              aria-hidden
            >
              {w}
            </span>
          ))}
        </div>
      ) : null}
      <div className={cn("grid grid-cols-7", gapClass)} role="grid" aria-label={monthLabel}>
        {leadingEmpties.map((id) => (
          <span key={id} className={cellSize} aria-hidden />
        ))}
        {dayNumbers.map((day) => {
          const iso = `${year}-${String(month + 1).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
          const data = byDate.get(iso);
          const level: HeatLevel = data?.level ?? 0;
          const moodUp = data?.moodUp ?? false;
          const isToday = iso === todayISO;
          const isFuture = iso > todayISO;
          const interactive = !isFuture && !!onSelect;
          return (
            <motion.button
              key={iso}
              type="button"
              role="gridcell"
              disabled={!interactive}
              onClick={() => interactive && onSelect?.(iso)}
              whileHover={
                interactive && !reduceMotion ? { scale: 1.08 } : undefined
              }
              transition={{ type: "spring", stiffness: 400, damping: 22 }}
              className={cn(
                cellSize,
                "relative flex items-center justify-center font-mono text-[10px] tabular-nums",
                "outline-none transition-colors",
                "focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 ring-offset-background",
                !interactive && "cursor-default",
                isFuture && "opacity-30",
                isToday && "ring-1 ring-foreground/40",
                level >= 3
                  ? "text-primary-foreground/90"
                  : level >= 1
                    ? "text-foreground/80"
                    : "text-muted-foreground/60",
              )}
              style={{
                backgroundColor: LEVEL_VAR[level],
                boxShadow: moodUp
                  ? "inset 0 0 0 1px var(--heat-mood)"
                  : undefined,
              }}
              aria-label={cellLabel(iso, data)}
              title={cellLabel(iso, data)}
            >
              {size === "lg" ? day : null}
            </motion.button>
          );
        })}
      </div>
    </div>
  );
}

function WeekRow({
  start,
  byDate,
  todayISO,
  onSelect,
  className,
}: {
  start: Date;
  byDate: Map<string, HeatCellData>;
  todayISO: string;
  onSelect?: (date: string) => void;
  className?: string;
}) {
  const reduceMotion = useReducedMotion();
  const days = Array.from({ length: 7 }, (_, i) => {
    const d = new Date(start);
    d.setUTCDate(d.getUTCDate() + i);
    return d;
  });
  return (
    <div className={cn("flex w-full justify-between gap-2", className)} role="grid">
      {days.map((d) => {
        const iso = formatISO(d);
        const data = byDate.get(iso);
        const level: HeatLevel = data?.level ?? 0;
        const moodUp = data?.moodUp ?? false;
        const isToday = iso === todayISO;
        const isFuture = iso > todayISO;
        const interactive = !isFuture && !!onSelect;
        const dow = WEEKDAYS[d.getUTCDay()];
        const day = d.getUTCDate();
        return (
          <div key={iso} className="flex flex-1 flex-col items-center gap-1.5">
            <span className="font-mono text-[10px] uppercase tracking-wider text-muted-foreground">
              {dow}
            </span>
            <motion.button
              type="button"
              role="gridcell"
              disabled={!interactive}
              onClick={() => interactive && onSelect?.(iso)}
              whileHover={
                interactive && !reduceMotion ? { scale: 1.05 } : undefined
              }
              transition={{ type: "spring", stiffness: 400, damping: 22 }}
              className={cn(
                "h-12 w-full max-w-14 rounded-lg",
                "relative flex items-center justify-center font-mono text-base tabular-nums",
                "outline-none transition-colors",
                "focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 ring-offset-background",
                !interactive && "cursor-default",
                isFuture && "opacity-30",
                isToday && "ring-1 ring-foreground/40",
                level >= 3
                  ? "text-primary-foreground/90"
                  : level >= 1
                    ? "text-foreground/80"
                    : "text-muted-foreground/60",
              )}
              style={{
                backgroundColor: LEVEL_VAR[level],
                boxShadow: moodUp
                  ? "inset 0 0 0 1px var(--heat-mood)"
                  : undefined,
              }}
              aria-label={cellLabel(iso, data)}
              title={cellLabel(iso, data)}
            >
              {day}
            </motion.button>
          </div>
        );
      })}
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
