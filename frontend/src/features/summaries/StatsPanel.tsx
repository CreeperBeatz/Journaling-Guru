import { useMemo } from "react";

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

import type { StatsResponse } from "./api";
import { MoodSparkline } from "./MoodSparkline";

interface Props {
  stats: StatsResponse | undefined;
  loading: boolean;
  className?: string;
}

// StatsPanel sits above the period tabs on the SummariesPage: a 90-day
// mood line, the user's top emotions in that window, total entries, and
// current streak. Everything is computed off daily summaries' metadata —
// inactive users see "no data yet" placeholders.
export function StatsPanel({ stats, loading, className }: Props) {
  const totalEntries = stats?.mood?.length ?? 0;
  const avg = useMemo(() => {
    if (!stats || stats.mood.length === 0) return null;
    const sum = stats.mood.reduce((acc, p) => acc + p.score, 0);
    return sum / stats.mood.length;
  }, [stats]);
  const streak = useMemo(() => computeStreak(stats?.mood ?? []), [stats]);

  if (loading) {
    return (
      <div className={cn("grid gap-4 md:grid-cols-2", className)}>
        <Skeleton className="h-40 w-full rounded-xl" />
        <Skeleton className="h-40 w-full rounded-xl" />
      </div>
    );
  }
  return (
    <div className={cn("grid gap-4 md:grid-cols-2", className)}>
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="font-serif text-base">Mood</CardTitle>
          <CardDescription>
            {stats && stats.mood.length > 0 ? (
              <>
                Average{" "}
                <span className="font-mono tabular-nums">
                  {avg !== null ? avg.toFixed(1) : "—"}
                </span>
                /10 across {stats.mood.length} day{stats.mood.length === 1 ? "" : "s"}
                {" "}in the last {stats.window_days} days.
              </>
            ) : (
              <>No daily summaries in the last {stats?.window_days ?? 90} days.</>
            )}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <MoodSparkline data={stats?.mood ?? []} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="font-serif text-base">Top emotions</CardTitle>
          <CardDescription>
            {stats && stats.emotions.length > 0
              ? `From your daily reflections in the last ${stats.window_days} days.`
              : "Your daily summaries' emotion tags will surface here as they accrue."}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-1.5">
          {stats && stats.emotions.length > 0 ? (
            <EmotionBars emotions={stats.emotions} />
          ) : (
            <p className="text-sm text-muted-foreground">No emotions tagged yet.</p>
          )}
        </CardContent>
      </Card>

      <Card className="md:col-span-2">
        <CardContent className="grid grid-cols-3 gap-4 py-4 text-center">
          <Stat label="Days reflected" value={String(totalEntries)} />
          <Stat label="Current streak" value={`${streak} day${streak === 1 ? "" : "s"}`} />
          <Stat
            label="Window"
            value={`${stats?.window_days ?? 90}d`}
          />
        </CardContent>
      </Card>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="space-y-0.5">
      <div className="font-mono text-xl tabular-nums">{value}</div>
      <div className="text-[11px] uppercase tracking-wider text-muted-foreground">
        {label}
      </div>
    </div>
  );
}

function EmotionBars({ emotions }: { emotions: { emotion: string; count: number }[] }) {
  const max = Math.max(...emotions.map((e) => e.count), 1);
  return (
    <ul className="space-y-1.5">
      {emotions.map((e) => {
        const pct = (e.count / max) * 100;
        return (
          <li key={e.emotion} className="flex items-center gap-3">
            <span className="w-24 shrink-0 truncate text-sm capitalize">
              {e.emotion}
            </span>
            <div className="relative h-2 flex-1 overflow-hidden rounded-full bg-muted">
              <div
                className="absolute inset-y-0 left-0 rounded-full bg-accent/70"
                style={{ width: `${pct}%` }}
              />
            </div>
            <span className="w-6 shrink-0 text-right font-mono text-xs tabular-nums text-muted-foreground">
              {e.count}
            </span>
          </li>
        );
      })}
    </ul>
  );
}

// computeStreak counts consecutive days with a daily summary, ending at
// today (or yesterday if today's hasn't fired yet). Walks the mood
// series backwards. Tolerant: today's summary may not exist yet (it
// fires the morning after), so we accept a 1-day gap at the head.
function computeStreak(mood: { local_date: string }[]): number {
  if (mood.length === 0) return 0;
  // mood is oldest→newest; reverse for backwards walk.
  const dates = mood.map((p) => p.local_date).reverse();
  const today = todayUTCDateString();
  const yesterday = addDaysUTC(today, -1);
  let cursor = dates[0];
  // Allow a 1-day grace at the head (today's summary not generated yet).
  if (cursor !== today && cursor !== yesterday) return 0;
  let streak = 1;
  for (let i = 1; i < dates.length; i++) {
    const expected = addDaysUTC(cursor, -1);
    if (dates[i] === expected) {
      cursor = dates[i];
      streak++;
    } else {
      break;
    }
  }
  return streak;
}

function todayUTCDateString(): string {
  const now = new Date();
  return now.toISOString().slice(0, 10);
}

function addDaysUTC(yyyymmdd: string, delta: number): string {
  const [y, m, d] = yyyymmdd.split("-").map(Number);
  const dt = new Date(Date.UTC(y, m - 1, d));
  dt.setUTCDate(dt.getUTCDate() + delta);
  return dt.toISOString().slice(0, 10);
}
