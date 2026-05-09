import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowDownRight, ArrowRight, ArrowUpRight, Sparkles, Target } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

import { ApiError } from "@/api/client";
import {
  ReflectionResponse,
  ReflectionTagRow,
  getThisWeekReflection,
} from "./api";
import { SmartShaperModal } from "@/features/goals/SmartShaperModal";
import { useSaveDailyInput } from "@/features/daily/hooks";
import type { DailyInput, DailyInputUpsertBody, TagDayLink } from "@/features/daily/api";

interface Props {
  // The day's daily_input (today). Used as the substrate for the
  // "surprise" answer — surprise lives in reflection_text, since the
  // FE doesn't have a separate column for it. Manual-wins ensures it
  // doesn't clobber any reflection text already written.
  dailyInput: DailyInput | null;
  tags: TagDayLink[];
}

const LOW_CONFIDENCE_THRESHOLD = 7;

// WeeklyReflection — Phase 7. Replaces the daily Manual/Chat tabs on
// the user's chosen reflection_weekday. Four steps:
//   1. Pattern view (top drainers/chargers + Δ + gratitudes)
//   2. Surprise prompt (free text → daily_inputs.reflection_text)
//   3. Action prompt (CTA → SMART shaper modal)
//   4. Active goals progress (kept-it counts, week-local)
//
// All non-LLM (the headline lives on /summary; the SMART shaper here
// is the only LLM touch, and it's user-triggered).
export function WeeklyReflection({ dailyInput, tags }: Props) {
  const reflection = useQuery<ReflectionResponse, ApiError>({
    queryKey: ["reflection", "this-week"],
    queryFn: getThisWeekReflection,
    staleTime: 60_000,
  });

  if (reflection.isPending) {
    return (
      <Card>
        <CardContent className="px-6 py-8 text-sm text-muted-foreground">
          Loading the week…
        </CardContent>
      </Card>
    );
  }
  if (reflection.isError) {
    return (
      <Card>
        <CardContent className="px-6 py-8 text-sm text-destructive">
          {reflection.error.message}
        </CardContent>
      </Card>
    );
  }
  const data = reflection.data!;

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <p className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground">
          Weekly reflection
        </p>
        <h1 className="font-serif text-h1">This week, looking back</h1>
        <p className="text-sm text-muted-foreground">
          {data.week_start} → {data.week_end} · {data.entry_count}{" "}
          {data.entry_count === 1 ? "logged day" : "logged days"}
        </p>
      </header>

      <PatternCard data={data} />
      <SurpriseCard dailyInput={dailyInput} tags={tags} />
      <ActionCard />
      <ActiveGoalsCard data={data} />
    </div>
  );
}

// ---------------- Pattern view ----------------

function PatternCard({ data }: { data: ReflectionResponse }) {
  const moodDelta = computeMoodDelta(data.mood_avg, data.mood_avg_prior);
  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="font-serif text-base">What the week looked like</CardTitle>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="flex flex-wrap items-baseline gap-x-6 gap-y-2">
          <div>
            <p className="text-[11px] uppercase tracking-wider text-muted-foreground">
              Avg mood
            </p>
            <p className="font-mono text-2xl tabular-nums">
              {fmtMood(data.mood_avg)}
              <span className="text-sm text-muted-foreground"> / 3</span>
            </p>
          </div>
          {moodDelta ? (
            <p
              className={cn(
                "inline-flex items-center gap-0.5 text-sm font-medium",
                moodDelta.direction === "up"
                  ? "text-accent"
                  : moodDelta.direction === "down"
                    ? "text-destructive/80"
                    : "text-muted-foreground",
              )}
            >
              {moodDelta.direction === "up" ? (
                <ArrowUpRight className="h-4 w-4" />
              ) : moodDelta.direction === "down" ? (
                <ArrowDownRight className="h-4 w-4" />
              ) : (
                <ArrowRight className="h-4 w-4" />
              )}
              {moodDelta.label}
            </p>
          ) : null}
        </div>

        <div className="grid gap-6 md:grid-cols-2">
          <DeltaTagTable
            title="Top drainers"
            tone="negative"
            rows={data.drainers}
            empty="Nothing surfaced as a drainer this week."
          />
          <DeltaTagTable
            title="Top chargers"
            tone="positive"
            rows={data.chargers}
            empty="Nothing surfaced as a charger this week."
          />
        </div>

        {data.gratitude_items.length > 0 ? (
          <div className="space-y-2">
            <h3 className="text-[11px] uppercase tracking-wider text-muted-foreground">
              Gratitude this week
            </h3>
            <ul className="space-y-1 text-sm">
              {data.gratitude_items.map((g) => (
                <li
                  key={g.local_date}
                  className="border-b border-border/40 pb-1 last:border-0"
                >
                  <span className="font-mono text-[11px] tabular-nums text-muted-foreground">
                    {g.local_date}
                  </span>{" "}
                  · {g.text}
                </li>
              ))}
            </ul>
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}

function DeltaTagTable({
  title,
  tone,
  rows,
  empty,
}: {
  title: string;
  tone: "positive" | "negative";
  rows: ReflectionTagRow[];
  empty: string;
}) {
  return (
    <div className="space-y-2">
      <h3
        className={cn(
          "text-xs font-medium uppercase tracking-wider",
          tone === "positive" ? "text-accent" : "text-destructive/80",
        )}
      >
        {title}
      </h3>
      {rows.length === 0 ? (
        <p className="text-xs italic text-muted-foreground">{empty}</p>
      ) : (
        <ul className="space-y-1">
          {rows.map((row) => {
            const deltaIcon =
              row.delta_vs_prior > 0 ? (
                <ArrowUpRight className="h-3 w-3" />
              ) : row.delta_vs_prior < 0 ? (
                <ArrowDownRight className="h-3 w-3" />
              ) : null;
            return (
              <li
                key={row.tag_id}
                className="flex items-baseline justify-between gap-3 border-b border-border/40 pb-1 text-sm last:border-0"
              >
                <span className="min-w-0 flex-1 truncate">
                  {row.label}
                  {row.appearances < LOW_CONFIDENCE_THRESHOLD ? (
                    <span className="ml-2 text-[10px] uppercase tracking-wider text-muted-foreground/70">
                      low confidence
                    </span>
                  ) : null}
                </span>
                <span className="inline-flex shrink-0 items-center gap-1.5 font-mono text-xs tabular-nums text-muted-foreground">
                  {row.appearances}d
                  {deltaIcon ? (
                    <span
                      className={cn(
                        "inline-flex items-center",
                        row.delta_vs_prior > 0
                          ? tone === "positive"
                            ? "text-accent"
                            : "text-destructive/80"
                          : tone === "positive"
                            ? "text-destructive/80"
                            : "text-accent",
                      )}
                    >
                      {deltaIcon}
                      {Math.abs(row.delta_vs_prior)}
                    </span>
                  ) : null}
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

// ---------------- Surprise prompt ----------------

function SurpriseCard({
  dailyInput,
  tags,
}: {
  dailyInput: DailyInput | null;
  tags: TagDayLink[];
}) {
  // Surprise text persists in daily_inputs.reflection_text — same field
  // the Manual tab uses, since on the reflection day we don't have a
  // separate Manual tab to hold it. Manual-wins on the backend keeps
  // any chat-extracted reflection text intact if the user comes back
  // and types here.
  const [text, setText] = useState(dailyInput?.reflection_text ?? "");
  const save = useSaveDailyInput();

  const drainerIds = tags.filter((t) => t.role === "drainer").map((t) => t.tag_id);
  const chargerIds = tags.filter((t) => t.role === "charger").map((t) => t.tag_id);

  const flush = () => {
    const trimmed = text.trim();
    if (trimmed === (dailyInput?.reflection_text ?? "").trim()) return;
    const body: DailyInputUpsertBody = {
      mood: dailyInput?.mood ?? null,
      drained_text: dailyInput?.drained_text ?? "",
      charged_text: dailyInput?.charged_text ?? "",
      gratitude_text: dailyInput?.gratitude_text ?? "",
      reflection_text: trimmed,
      drained_tag_ids: drainerIds,
      charged_tag_ids: chargerIds,
    };
    save.mutate(body);
  };

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="font-serif text-base">
          Did anything surprise you this week?
        </CardTitle>
        <p className="text-xs italic text-muted-foreground">
          Free text — optional. Saved to today's reflection.
        </p>
      </CardHeader>
      <CardContent>
        <Textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          onBlur={flush}
          rows={3}
          maxLength={4000}
          placeholder="Anything stood out — meetings showed up more than usual, sleep slipped, a small win you almost missed…"
          className="border-transparent bg-transparent px-0 leading-prose focus-visible:rounded-none focus-visible:border-b focus-visible:border-b-border focus-visible:ring-0 focus-visible:ring-offset-0"
        />
      </CardContent>
    </Card>
  );
}

// ---------------- Action prompt ----------------

function ActionCard() {
  const [shaperOpen, setShaperOpen] = useState(false);
  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="font-serif text-base">
          Want to act on something?
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <p className="text-sm text-muted-foreground">
          Pick one drainer or charger from the table above and commit to a
          small change. The shaper turns it into a yes/no daily check-in
          you can measure for a couple of weeks.
        </p>
        <Button onClick={() => setShaperOpen(true)} className="gap-1.5">
          <Sparkles className="h-4 w-4" />
          Shape a goal
        </Button>
        <SmartShaperModal open={shaperOpen} onOpenChange={setShaperOpen} />
      </CardContent>
    </Card>
  );
}

// ---------------- Active goals progress ----------------

function ActiveGoalsCard({ data }: { data: ReflectionResponse }) {
  if (data.active_goals.length === 0) return null;
  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="font-serif text-base">
          Active goals — this week
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-2">
        {data.active_goals.map((g) => (
          <div
            key={g.id}
            className="flex items-baseline justify-between gap-3 rounded-md border border-border/60 px-3 py-2"
          >
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm font-medium">{g.title}</p>
              <p className="text-[11px] text-muted-foreground">
                Day {g.day_index} of {g.total_days} · ends {g.end_date}
              </p>
            </div>
            <div className="shrink-0 text-right">
              <p className="font-mono text-sm tabular-nums">
                {g.kept_count}
                <span className="text-muted-foreground">/{g.answered_count || 7}</span>
              </p>
              <p className="text-[10px] uppercase tracking-wider text-muted-foreground">
                kept
              </p>
            </div>
          </div>
        ))}
        <p className="pt-1 text-[11px] italic text-muted-foreground">
          <Target className="mr-1 inline h-3 w-3" />
          Counts reflect this week only.
        </p>
      </CardContent>
    </Card>
  );
}

// ---------------- helpers ----------------

function fmtMood(score: number | null): string {
  if (score === null) return "—";
  return score.toFixed(1);
}

function computeMoodDelta(
  current: number | null,
  prior: number | null,
): { direction: "up" | "down" | "flat"; label: string } | null {
  if (current === null || prior === null) return null;
  const diff = current - prior;
  if (Math.abs(diff) < 0.05) return { direction: "flat", label: "flat vs last week" };
  const sign = diff > 0 ? "+" : "";
  return {
    direction: diff > 0 ? "up" : "down",
    label: `${sign}${diff.toFixed(1)} vs last week`,
  };
}
