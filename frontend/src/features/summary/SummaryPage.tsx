import { useState } from "react";

import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useSummaries } from "@/features/summaries/hooks";

import { WeeklyLetter } from "./WeeklyLetter";
import { ByQuestionTimeline } from "./ByQuestionTimeline";
import { TrendsDashboard } from "./TrendsDashboard";

type Tab = "trends" | "by-question";

/**
 * /summary — two-tab surface:
 *   - Trends: WeeklyLetter (hero) + collapsible TrendsDashboard
 *     (StatTiles + MoodLine + WordCloud + GuruNote).
 *   - By Question: question rail + vertical timeline.
 *
 * The most recent weekly summary drives the letter and seeds the
 * dashboard's GuruNote / accent pick. When no weekly summary exists
 * yet, WeeklyLetter renders its empty-state copy and the dashboard
 * still surfaces stats from the daily summary stream.
 */
export function SummaryPage() {
  const [tab, setTab] = useState<Tab>("trends");
  const weekly = useSummaries("week", 1);

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
          <TrendsDashboard weekly={latestLetter} />
        </TabsContent>

        <TabsContent value="by-question" className="m-0">
          <ByQuestionTimeline />
        </TabsContent>
      </Tabs>
    </div>
  );
}
