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
  // True when this date is the week_end of a *completed* weekly
  // reflection. Renders a small accent dot in the cell so the user
  // can spot which days have a reflection to view in History.
  hasWeeklyReflection?: boolean;
}

export type HeatView = "year" | "month";

export interface HeatGridProps {
  cells: HeatCellData[];
  view?: HeatView;
  /** Anchor date (default: today). Year view shows the 12 months
   *  ending at the anchor's month; month view shows the anchor's
   *  month. */
  anchor?: string;
  /** Click on a day cell in month view → open that day. */
  onSelect?: (date: string) => void;
  /** Click on a month tile in year view → drill into month view. */
  onMonthClick?: (anchorISO: string) => void;
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

const WEEKDAYS = ["M", "T", "W", "T", "F", "S", "S"];

export function HeatGrid({
  cells,
  view = "year",
  anchor,
  onSelect,
  onMonthClick,
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

  if (view === "month") {
    return (
      <div className={cn("mx-auto w-full max-w-2xl px-2 sm:px-8 md:px-12", className)}>
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
  /** Month-view variant: clicking a day cell opens that day. */
  onSelect?: (iso: string) => void;
  size: "sm" | "lg";
  showWeekdays?: boolean;
}

function MonthGrid({
  year,
  month,
  byDate,
  todayISO,
  onMonthClick,
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
  // Week starts on Monday → ends on Sunday. Map JS getUTCDay() (0=Sun)
  // to a 0=Mon..6=Sun offset.
  const firstDow = (startOfMonth(year, month).getUTCDay() + 6) % 7;
  // Month view (lg) goes fluid: cells fill their grid column and stay
  // square via aspect-ratio so the calendar grows with the container.
  // Year-tile (sm) keeps its compact fixed-pixel grid.
  const cellSize =
    size === "lg"
      ? "aspect-square w-full rounded-md"
      : "h-5 w-5 rounded-[3px]";
  const gapClass = size === "lg" ? "gap-1.5" : "gap-[3px]";
  const labelClass =
    size === "lg" ? "text-sm font-medium" : "text-xs font-medium";

  // Build week rows: 6 rows × 7 cells (max). Each row is a Monday-start
  // week (ends Sunday). Leading empties pad the 1st to its real weekday;
  // trailing empties round out the last row.
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
        <div className={cn("mb-1 grid w-full grid-cols-7", gapClass)}>
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
      <div className="flex w-full flex-col" style={{ gap: size === "lg" ? "0.375rem" : "3px" }}>
        {weeks.map((week) => (
          <div key={week.id} className={cn("grid w-full grid-cols-7", gapClass)}>
            {week.cells.map((c, idx) => {
              if (!c.iso) {
                return <span key={`e-${week.id}-${idx}`} className={cellSize} aria-hidden />;
              }
              const data = byDate.get(c.iso);
              const level: HeatLevel = data?.level ?? 0;
              const moodUp = data?.moodUp ?? false;
              const hasReflection = data?.hasWeeklyReflection ?? false;
              const isToday = c.iso === todayISO;
              const isFuture = c.iso > todayISO;
              const interactive = !!onSelect && !isFuture;
              const cellClass = cn(
                cellSize,
                "relative flex items-center justify-center font-mono tabular-nums",
                size === "lg" ? "text-sm" : "text-[10px]",
                "transition-colors",
                isFuture && "opacity-30",
                isToday && "ring-1 ring-foreground/40",
                interactive && "hover:ring-2 hover:ring-ring outline-none focus-visible:ring-2 focus-visible:ring-ring",
                level >= 1
                  ? "text-foreground/80"
                  : "text-muted-foreground/60",
              );
              const styleProps = {
                backgroundColor: LEVEL_VAR[level],
                boxShadow: moodUp ? "inset 0 0 0 1px var(--heat-mood)" : undefined,
              } as const;
              const reflectionDot = hasReflection ? (
                <span
                  aria-hidden
                  className={cn(
                    "absolute rounded-full bg-accent",
                    size === "lg"
                      ? "right-1 top-1 h-1.5 w-1.5"
                      : "right-0.5 top-0.5 h-1 w-1",
                  )}
                />
              ) : null;
              if (interactive) {
                return (
                  <button
                    key={c.iso}
                    type="button"
                    onClick={() => onSelect?.(c.iso!)}
                    className={cellClass}
                    style={styleProps}
                    title={cellLabel(c.iso, data)}
                    aria-label={cellLabel(c.iso, data)}
                  >
                    {reflectionDot}
                    {size === "lg" ? c.day : null}
                  </button>
                );
              }
              return (
                <span
                  key={c.iso}
                  role="gridcell"
                  title={cellLabel(c.iso, data)}
                  aria-label={cellLabel(c.iso, data)}
                  className={cellClass}
                  style={styleProps}
                >
                  {reflectionDot}
                  {size === "lg" ? c.day : null}
                </span>
              );
            })}
          </div>
        ))}
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

  // lg variant is the standalone month view — its label already lives in
  // the prev/next nav above, so we don't repeat it here.
  return (
    <div className="flex w-full flex-col items-stretch">
      {size === "lg" ? null : (
        <p className={cn("mb-2 self-start", labelClass)}>{monthLabel}</p>
      )}
      {cells}
    </div>
  );
}

function cellLabel(iso: string, data?: HeatCellData): string {
  if (!data || data.level === 0) {
    if (data?.hasWeeklyReflection) return `${iso}: weekly reflection`;
    return `${iso}: no entry`;
  }
  const parts = [iso, "entry logged"];
  if (data.moodUp) parts.push("mood up");
  if (data.hasWeeklyReflection) parts.push("weekly reflection");
  return parts.join(" · ");
}

/**
 * Build a chronological array of <HeatCellData> from a raw heatmap
 * response. A day counts as filled (`level === 1`) if it has any manual
 * signal — a journal entry, a daily-input field (mood / drained /
 * charged / gratitude / reflection), or a chat session with substantive
 * turns. `moodUp` is mood >= 1 on the signed -2..+2 Energy Audit scale
 * (any positive day; 0=neutral does not count).
 */
export function buildHeatCells(
  days: {
    local_date: string;
    answered: number;
    chat_turns: number;
    mood?: number | null;
    has_inputs?: boolean;
  }[],
  reflectionDates?: string[],
): HeatCellData[] {
  const reflectionSet = new Set(reflectionDates ?? []);
  const seen = new Set<string>();
  const cells: HeatCellData[] = days.map((d) => {
    seen.add(d.local_date);
    return {
      date: d.local_date,
      level: d.answered > 0 || d.chat_turns >= 3 || !!d.has_inputs ? 1 : 0,
      moodUp: d.mood != null && d.mood >= 1,
      hasWeeklyReflection: reflectionSet.has(d.local_date),
    };
  });
  // A weekly reflection is its own signal — a user can complete the
  // wizard without touching journal / daily inputs / chat that day, so
  // the date won't appear in `days`. Emit a level-0 cell for those so
  // the accent dot still lands on the heatmap.
  for (const date of reflectionSet) {
    if (seen.has(date)) continue;
    cells.push({ date, level: 0, moodUp: false, hasWeeklyReflection: true });
  }
  return cells;
}
