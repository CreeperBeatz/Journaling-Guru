import { useMemo } from "react";

import { Card, CardContent } from "@/components/ui/card";
import { GuruNote } from "@/components/ui/guru-note";
import { buildHeatCells } from "@/components/ui/heat-grid";
import { computeStreak } from "@/components/ui/streak-badge";
import { useStats } from "@/features/summaries/hooks";
import { useHeatmap } from "@/features/journal/hooks";
import type { Summary } from "@/features/summaries/api";

import { MoodLine } from "./MoodLine";
import { StatTiles } from "./StatTiles";
import { WordCloud } from "./WordCloud";

export interface TrendsDashboardProps {
  /** Latest weekly summary; drives the GuruNote preview + noticed pick. */
  weekly: Summary | null;
}

/**
 * Trends-tab dashboard, sits below the WeeklyLetter. Folds inside a
 * `<details>` because the letter is the hero — most users glance at
 * the tiles, not the whole panel.
 */
export function TrendsDashboard({ weekly }: TrendsDashboardProps) {
  const heatmap = useHeatmap();
  const stats = useStats(90);

  const cells = useMemo(
    () => (heatmap.data ? buildHeatCells(heatmap.data.days) : []),
    [heatmap.data],
  );
  const today =
    heatmap.data?.today ?? new Date().toISOString().slice(0, 10);

  const streak = useMemo(() => computeStreak(cells, today), [cells, today]);

  const daysThisWeek = useMemo(() => {
    if (cells.length === 0) return 0;
    const cutoff = addDays(today, -6);
    return cells.filter((c) => c.date >= cutoff && c.date <= today && c.level === 1).length;
  }, [cells, today]);

  const moodAvg = useMemo(() => {
    const recent = (stats.data?.mood ?? []).slice(-30);
    if (recent.length === 0) return null;
    const sum = recent.reduce((acc, p) => acc + p.score, 0);
    return sum / recent.length;
  }, [stats.data]);

  const tiles = [
    { label: "Streak", value: streak === 0 ? "—" : `${streak}d` },
    { label: "This week", value: `${daysThisWeek}/7` },
    {
      label: "Avg mood",
      value: moodAvg === null ? "—" : moodAvg.toFixed(1),
      hint: moodAvg === null ? undefined : "30-day",
    },
  ];

  // GuruNote: first paragraph of the weekly letter is a usable
  // "noticed" preview until the backend exposes a dedicated narrative
  // field. Strip leading framing ("Dear you,") if present.
  const noticed = useMemo(() => {
    if (!weekly?.body) return null;
    const para = weekly.body
      .split(/\n{2,}/)
      .map((s) => s.trim())
      .find((s) => s.length > 0 && !/^dear\b/i.test(s));
    return para ?? null;
  }, [weekly]);

  // Pick the first topic from the weekly summary metadata as the
  // accented "noticed" word in the cloud, when available.
  const noticedWord =
    weekly?.metadata?.topics?.[0] ??
    weekly?.metadata?.emotions?.[0] ??
    undefined;

  return (
    <details className="group rounded-xl border border-border bg-card">
      <summary
        className="flex cursor-pointer items-center justify-between px-5 py-3 text-sm select-none list-none [&::-webkit-details-marker]:hidden"
      >
        <span className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground">
          Stats this week
        </span>
        <span
          className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground transition-transform group-open:rotate-90"
          aria-hidden
        >
          ›
        </span>
      </summary>
      <Card className="rounded-none border-0 border-t border-border/60 shadow-none">
        <CardContent className="space-y-6 px-5 py-6">
          <StatTiles tiles={tiles} />

          <section className="space-y-2">
            <p className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground">
              Mood (last {stats.data?.mood.length ?? 0} days)
            </p>
            <MoodLine data={stats.data?.mood ?? []} />
          </section>

          <section className="space-y-2">
            <p className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground">
              Words you used
            </p>
            <WordCloud
              words={stats.data?.emotions ?? []}
              noticed={noticedWord}
            />
          </section>

          {noticed ? (
            <GuruNote eyebrow="Noticed">{noticed}</GuruNote>
          ) : null}
        </CardContent>
      </Card>
    </details>
  );
}

function addDays(iso: string, n: number): string {
  const [y, m, d] = iso.split("-").map(Number);
  const dt = new Date(Date.UTC(y, m - 1, d));
  dt.setUTCDate(dt.getUTCDate() + n);
  const yy = dt.getUTCFullYear();
  const mm = String(dt.getUTCMonth() + 1).padStart(2, "0");
  const dd = String(dt.getUTCDate()).padStart(2, "0");
  return `${yy}-${mm}-${dd}`;
}
