import { Link } from "react-router-dom";
import { useState } from "react";

import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";

import { useStats, useSummaries } from "./hooks";
import type { PeriodType, Summary } from "./api";
import { StatsPanel } from "./StatsPanel";

const PERIODS: { value: PeriodType; label: string }[] = [
  { value: "day", label: "Daily" },
  { value: "week", label: "Weekly" },
  { value: "month", label: "Monthly" },
  { value: "year", label: "Yearly" },
];

export function SummariesPage() {
  const [period, setPeriod] = useState<PeriodType>("day");
  const stats = useStats(90);

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <p className="text-xs uppercase tracking-wider text-muted-foreground">Summaries</p>
        <h1 className="font-serif text-h1">Reflections</h1>
        <p className="text-sm text-muted-foreground">
          Daily, weekly, monthly, and yearly reflections written automatically
          from your journal entries.
        </p>
      </header>

      <StatsPanel stats={stats.data} loading={stats.isPending} />

      <Tabs value={period} onValueChange={(v) => setPeriod(v as PeriodType)} className="space-y-4">
        <TabsList className="bg-muted/40">
          {PERIODS.map((p) => (
            <TabsTrigger key={p.value} value={p.value}>
              {p.label}
            </TabsTrigger>
          ))}
        </TabsList>
        {PERIODS.map((p) => (
          <TabsContent key={p.value} value={p.value} className="m-0">
            <SummaryList period={p.value} />
          </TabsContent>
        ))}
      </Tabs>
    </div>
  );
}

function SummaryList({ period }: { period: PeriodType }) {
  const { data, isPending, isError } = useSummaries(period);

  if (isPending) {
    return <p className="text-sm text-muted-foreground">Loading…</p>;
  }
  if (isError) {
    return <p className="text-sm text-destructive">Couldn't load summaries.</p>;
  }
  if (!data || data.length === 0) {
    return (
      <Card className="border-dashed">
        <CardHeader className="pb-2">
          <CardTitle className="font-serif text-base">No {periodNounPlural(period)} yet</CardTitle>
          <CardDescription>{emptyHint(period)}</CardDescription>
        </CardHeader>
      </Card>
    );
  }
  return (
    <ul className="space-y-3">
      {data.map((s) => (
        <li key={s.id}>
          <SummaryCard summary={s} />
        </li>
      ))}
    </ul>
  );
}

function SummaryCard({ summary: s }: { summary: Summary }) {
  const meta = s.metadata ?? {};
  return (
    <Link
      to={`/summaries/${s.id}`}
      className={cn(
        "group block rounded-xl border border-border bg-card p-4 transition-colors",
        "hover:border-accent/40 hover:bg-secondary/30",
      )}
    >
      <div className="flex items-baseline justify-between gap-3">
        <h3 className="font-serif text-lg leading-tight group-hover:underline-offset-4">
          {periodHeading(s.period_type, s.period_start, s.period_end)}
        </h3>
        {meta.mood_label ? (
          <span
            className={cn(
              "shrink-0 rounded-full border border-border px-2 py-0.5 text-[11px] uppercase tracking-wider",
              moodPillClass(meta.mood_label),
            )}
          >
            {meta.mood_label}
          </span>
        ) : null}
      </div>
      <p className="mt-2 line-clamp-3 text-sm text-muted-foreground">
        {previewText(s.body)}
      </p>
      <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
        {meta.mood_score != null ? (
          <span className="font-mono tabular-nums">mood {meta.mood_score.toFixed(1)}/10</span>
        ) : null}
        {meta.entry_count ? (
          <span className="font-mono tabular-nums">{meta.entry_count} entries</span>
        ) : null}
        {meta.emotions && meta.emotions.length > 0 ? (
          <span className="capitalize">{meta.emotions.slice(0, 3).join(" · ")}</span>
        ) : null}
      </div>
    </Link>
  );
}

function periodNounPlural(p: PeriodType): string {
  switch (p) {
    case "day":
      return "daily reflections";
    case "week":
      return "weekly reflections";
    case "month":
      return "monthly reflections";
    case "year":
      return "yearly reflections";
  }
}

function emptyHint(p: PeriodType): string {
  switch (p) {
    case "day":
      return "Daily reflections appear the morning after you write — give it a day or two.";
    case "week":
      return "Weekly reflections fire the morning after each week ends. Sundays!";
    case "month":
      return "Monthly reflections arrive on the 1st of next month.";
    case "year":
      return "Yearly reflections arrive on January 1st.";
  }
}

function moodPillClass(label: string): string {
  switch (label.toLowerCase()) {
    case "positive":
      return "border-accent/40 text-accent";
    case "negative":
      return "border-destructive/40 text-destructive";
    default:
      return "text-muted-foreground";
  }
}

function previewText(body: string): string {
  // Strip the most-common markdown noise so the preview reads as prose.
  return body
    .replace(/^#+\s+/gm, "")
    .replace(/[*_`>]/g, "")
    .replace(/\s+/g, " ")
    .trim()
    .slice(0, 240);
}

function periodHeading(periodType: string, start: string, end: string): string {
  if (periodType === "day") {
    const [y, m, d] = start.split("-").map(Number);
    return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString(undefined, {
      weekday: "short",
      month: "long",
      day: "numeric",
      year: "numeric",
      timeZone: "UTC",
    });
  }
  if (periodType === "year") {
    return start.slice(0, 4);
  }
  if (periodType === "month") {
    const [y, m] = start.split("-").map(Number);
    return new Date(Date.UTC(y, m - 1, 1)).toLocaleDateString(undefined, {
      month: "long",
      year: "numeric",
      timeZone: "UTC",
    });
  }
  // weekly
  const [y1, m1, d1] = start.split("-").map(Number);
  const [y2, m2, d2] = end.split("-").map(Number);
  const startDate = new Date(Date.UTC(y1, m1 - 1, d1));
  const endDate = new Date(Date.UTC(y2, m2 - 1, d2));
  const sameYear = y1 === y2;
  const sameMonth = sameYear && m1 === m2;
  if (sameMonth) {
    return `${startDate.toLocaleDateString(undefined, { month: "long", day: "numeric", timeZone: "UTC" })} – ${d2}, ${y1}`;
  }
  if (sameYear) {
    return `${startDate.toLocaleDateString(undefined, { month: "short", day: "numeric", timeZone: "UTC" })} – ${endDate.toLocaleDateString(undefined, { month: "short", day: "numeric", timeZone: "UTC" })}, ${y1}`;
  }
  return `${startDate.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric", timeZone: "UTC" })} – ${endDate.toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric", timeZone: "UTC" })}`;
}
