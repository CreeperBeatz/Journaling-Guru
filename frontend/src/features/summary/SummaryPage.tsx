import { useState } from "react";

import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useSummaries, useStats } from "@/features/summaries/hooks";
import { StatsPanel } from "@/features/summaries/StatsPanel";

import { WeeklyLetter } from "./WeeklyLetter";
import { ByQuestionTimeline } from "./ByQuestionTimeline";

type Tab = "trends" | "by-question";

/**
 * /summary — two-tab surface:
 *   - Trends: WeeklyLetter (hero) + folded dashboard (stats panel for now;
 *     WordCloud / MoodLine / GuruNote land in step 6).
 *   - By Question: question rail + vertical timeline.
 *
 * The most recent weekly summary drives the letter. When no weekly
 * summary exists yet (new users, mid-week before the first Sunday),
 * WeeklyLetter renders its empty-state copy.
 */
export function SummaryPage() {
  const [tab, setTab] = useState<Tab>("trends");
  const weekly = useSummaries("week", 1);
  const stats = useStats(90);

  const latestLetter = weekly.data?.[0] ?? null;

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <p className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground">
          Summary
        </p>
        <h1 className="font-serif text-h1">Patterns over time</h1>
      </header>

      <Tabs value={tab} onValueChange={(v) => setTab(v as Tab)} className="space-y-6">
        <TabsList>
          <TabsTrigger value="trends">Trends</TabsTrigger>
          <TabsTrigger value="by-question">By question</TabsTrigger>
        </TabsList>

        <TabsContent value="trends" className="m-0 space-y-6">
          <WeeklyLetter summary={latestLetter} loading={weekly.isPending} />
          {/* Step 6 will replace this with WordCloud + MoodLine + StatTiles
              + GuruNote inside a collapsible Card. For now, surface the
              existing stats panel so the data isn't dropped. */}
          <StatsPanel stats={stats.data} loading={stats.isPending} />
        </TabsContent>

        <TabsContent value="by-question" className="m-0">
          <ByQuestionTimeline />
        </TabsContent>
      </Tabs>
    </div>
  );
}
