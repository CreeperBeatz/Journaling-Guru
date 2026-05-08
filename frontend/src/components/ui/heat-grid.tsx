import { useMemo } from "react";
import { motion, useReducedMotion } from "motion/react";

import { cn } from "@/lib/utils";

// Binary: a day either has an entry or doesn't. We expect a similar
// volume of answers each day, so a 0-4 ramp is noise — Duolingo-style
// done/not-done is the right primitive.
export type HeatLevel = 0 | 1;

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
  /** Click on a day cell — only fires in week view. Year/month views
   *  drill down via `onMonthClick` / `onWeekClick` instead. */
  onSelect?: (date: string) => void;
  /** Click on a month tile in year view → drill into month view. */
  onMonthClick?: (anchorISO: string) => void;
  /** Click on a week row in month view → drill into week view. */
  onWeekClick?: (anchorISO: string) => void;
  className?: string;
}

const LEVEL_VAR: Record<HeatLevel, string> = {
  0: "var(--heat-empty)",
  1: "hsl(var(--primary))",
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
  onMonthClick,
  onWeekClick,
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
          onWeekClick={onWeekClick}
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
          onMonthClick={onMonthClick}
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
  /** Year-view variant: clicking the tile drills into month view. */
  onMonthClick?: (anchorISO: string) => void;
  /** Month-view variant: clicking a week row drills into week view. */
  onWeekClick?: (anchorISO: string) => void;
  size: "sm" | "lg";
  showWeekdays?: boolean;
}

function MonthGrid({
  year,
  month,
  byDate,
  todayISO,
  onMonthClick,
  onWeekClick,
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
  const cellSize =
    size === "lg" ? "h-10 w-10 rounded-md" : "h-5 w-5 rounded-[3px]";
  const gapClass = size === "lg" ? "gap-1.5" : "gap-[3px]";
  const labelClass =
    size === "lg" ? "text-sm font-medium" : "text-xs font-medium";

  // Build week rows: 6 rows × 7 cells (max). Each row is a Sunday-start
  // week. Leading empties pad the 1st to its real weekday; trailing
  // empties round out the last row.
  const totalCells = firstDow + totalDays;
  const totalRows = Math.ceil(totalCells / 7);
  const weeks: { id: string; cells: { iso: string | null; day: number | null }[] }[] = [];
  for (let r = 0; r < totalRows; r++) {
    const cells: { iso: string | null; day: number | null }[] = [];
    for (let c = 0; c < 7; c++) {
      const cellIndex = r * 7 + c;
      const dayNum = cellIndex - firstDow + 1;
      if (dayNum < 1 || dayNum > totalDays) {
        cells.push({ iso: null, day: null });
      } else {
        const iso = `${year}-${String(month + 1).padStart(2, "0")}-${String(dayNum).padStart(2, "0")}`;
        cells.push({ iso, day: dayNum });
      }
    }
    // Anchor each week on its first non-empty cell — onWeekClick uses
    // this to set the week-view anchor to a real day inside the month.
    const firstReal = cells.find((c) => c.iso !== null);
    weeks.push({ id: firstReal?.iso ?? `r-${r}`, cells });
  }

  const cells = (
    <>
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
      <div className="flex flex-col" style={{ gap: size === "lg" ? "0.375rem" : "3px" }}>
        {weeks.map((week) => {
          const weekAnchor = week.cells.find((c) => c.iso)?.iso ?? null;
          const interactiveRow = !!onWeekClick && !!weekAnchor;
          const RowTag: keyof JSX.IntrinsicElements = interactiveRow ? "button" : "div";
          return (
            <RowTag
              key={week.id}
              type={interactiveRow ? "button" : undefined}
              onClick={
                interactiveRow ? () => onWeekClick?.(weekAnchor!) : undefined
              }
              className={cn(
                "grid grid-cols-7",
                gapClass,
                interactiveRow &&
                  "rounded-md transition-colors hover:bg-secondary/40 -mx-1 px-1 py-0.5 outline-none focus-visible:ring-2 focus-visible:ring-ring",
              )}
              role={interactiveRow ? "button" : undefined}
              aria-label={interactiveRow ? `Week of ${weekAnchor}` : undefined}
            >
              {week.cells.map((c, idx) => {
                if (!c.iso) {
                  return <span key={`e-${week.id}-${idx}`} className={cellSize} aria-hidden />;
                }
                const data = byDate.get(c.iso);
                const level: HeatLevel = data?.level ?? 0;
                const moodUp = data?.moodUp ?? false;
                const isToday = c.iso === todayISO;
                const isFuture = c.iso > todayISO;
                return (
                  <span
                    key={c.iso}
                    role="gridcell"
                    title={cellLabel(c.iso, data)}
                    aria-label={cellLabel(c.iso, data)}
                    className={cn(
                      cellSize,
                      "relative flex items-center justify-center font-mono text-[10px] tabular-nums",
                      "transition-colors",
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
                  >
                    {size === "lg" ? c.day : null}
                  </span>
                );
              })}
            </RowTag>
          );
        })}
      </div>
    </>
  );

  // Year-view tiles wrap the whole month in a button. Hover lifts the
  // tile slightly so the affordance reads on desktop.
  if (onMonthClick && size === "sm") {
    const monthAnchor = `${year}-${String(month + 1).padStart(2, "0")}-01`;
    return (
      <motion.button
        type="button"
        onClick={() => onMonthClick(monthAnchor)}
        whileHover={!reduceMotion ? { y: -2 } : undefined}
        transition={{ type: "spring", stiffness: 380, damping: 28 }}
        className={cn(
          "flex flex-col items-start rounded-lg p-2 text-left",
          "transition-colors hover:bg-secondary/40",
          "outline-none focus-visible:ring-2 focus-visible:ring-ring",
        )}
        aria-label={`Open ${monthLabel}`}
      >
        <p className={cn("mb-2", labelClass)}>{monthLabel}</p>
        {cells}
      </motion.button>
    );
  }

  return (
    <div className="flex flex-col items-center">
      <p className={cn("mb-2 self-start", labelClass)}>{monthLabel}</p>
      {cells}
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
                level === 1
                  ? "text-primary-foreground/90"
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
  if (!data || data.level === 0) return `${iso}: no entry`;
  const parts = [iso, "entry logged"];
  if (data.moodUp) parts.push("mood up");
  return parts.join(" · ");
}

/**
 * Build a chronological array of <HeatCellData> from a raw heatmap
 * response. A day counts as filled (`level === 1`) if it has at least
 * one journal entry OR a chat session with substantive turns.
 * `moodUp` is mood_score >= 7 (top tertile of the 1-10 scale).
 */
export function buildHeatCells(
  days: { local_date: string; answered: number; chat_turns: number; mood?: number | null }[],
): HeatCellData[] {
  return days.map((d) => ({
    date: d.local_date,
    level: d.answered > 0 || d.chat_turns >= 3 ? 1 : 0,
    moodUp: (d.mood ?? 0) >= 7,
  }));
}
