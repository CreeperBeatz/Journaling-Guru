import { Flame } from "lucide-react";

import { cn } from "@/lib/utils";

export interface StreakBadgeProps {
  days: number;
  className?: string;
}

/**
 * Small pill rendering the current journaling streak. Lives in History
 * page header. Number is `font-mono tabular-nums` and `text-accent`; the
 * pill background is a soft accent wash. Zero-day streaks render a
 * muted "—" — they're not a milestone, but the slot stays so the
 * header doesn't reflow when the streak resets.
 */
export function StreakBadge({ days, className }: StreakBadgeProps) {
  const isZero = days === 0;
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-sm",
        isZero
          ? "bg-muted text-muted-foreground"
          : "bg-accent/10 text-foreground",
        className,
      )}
      title={isZero ? "No active streak" : `${days}-day streak`}
    >
      <Flame
        className={cn(
          "h-3.5 w-3.5",
          isZero ? "opacity-60" : "text-accent",
        )}
        aria-hidden
      />
      <span className="font-mono tabular-nums text-accent" aria-hidden>
        {isZero ? "—" : days}
      </span>
      <span className="text-muted-foreground">day streak</span>
    </span>
  );
}

/**
 * Compute current streak (consecutive days back from today with level >= 1).
 * Pass cells in any order; sorting + today-anchoring happens internally.
 * `today` is the user's local-today (YYYY-MM-DD) — passed in so the streak
 * lines up with the user's day-rollover, not the browser's UTC midnight.
 */
export function computeStreak(
  cells: { date: string; level: number }[],
  today: string,
): number {
  const byDate = new Map(cells.map((c) => [c.date, c.level]));
  let streak = 0;
  // Walk back from today. If today hasn't been logged yet, don't break
  // the streak — only a missed full day (yesterday or earlier) does.
  const start = parseISO(today);
  const todayLevel = byDate.get(today) ?? 0;
  const offset = todayLevel >= 1 ? 0 : 1;
  for (let i = 0; i < 366; i++) {
    const d = new Date(start);
    d.setUTCDate(d.getUTCDate() - (i + offset));
    const iso = formatISO(d);
    const level = byDate.get(iso) ?? 0;
    if (level >= 1) streak += 1;
    else break;
  }
  return streak;
}

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
