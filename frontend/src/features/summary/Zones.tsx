import { Link } from "react-router-dom";
import { ArrowDownRight, ArrowRight, ArrowUpRight, Target } from "lucide-react";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";

import type { Goal } from "@/features/goals/api";

import type {
  DailyMoodPoint,
  TagAggregate,
  Zone1GoalStatus,
  Zone1Response,
  Zone2Response,
  Zone3Response,
} from "./api";

// Spec: tags with fewer than ~7 appearances get a faint "low confidence"
// badge — not hidden, just flagged. Threshold is the same in Zone 2 and
// the weekly reflection view.
const LOW_CONFIDENCE_THRESHOLD = 7;

// ---------- Zone 1 ----------

export function Zone1Card({ data }: { data: Zone1Response }) {
  if (!data.has_baseline) {
    return <BaselineCard data={data} />;
  }

  const delta = computeMoodDelta(data.mood_avg_7d, data.mood_avg_prior_7d);

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="font-serif text-base">At a glance</CardTitle>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-3">
          <div className="min-w-0 flex-1">
            <p className="text-[11px] uppercase tracking-wider text-muted-foreground">
              Mood — last 30 days
            </p>
            <Sparkline points={data.mood} />
          </div>
          <div className="shrink-0 space-y-0.5 text-right">
            <p className="text-[11px] uppercase tracking-wider text-muted-foreground">
              7-day avg
            </p>
            <p className="font-mono text-2xl tabular-nums">
              {fmtMood(data.mood_avg_7d)}
            </p>
            {delta ? (
              <p
                className={cn(
                  "inline-flex items-center gap-0.5 text-xs font-medium",
                  delta.direction === "up"
                    ? "text-accent"
                    : delta.direction === "down"
                      ? "text-destructive/80"
                      : "text-muted-foreground",
                )}
              >
                {delta.direction === "up" ? (
                  <ArrowUpRight className="h-3 w-3" />
                ) : delta.direction === "down" ? (
                  <ArrowDownRight className="h-3 w-3" />
                ) : (
                  <ArrowRight className="h-3 w-3" />
                )}
                {delta.label}
              </p>
            ) : null}
          </div>
        </div>

        <div className="rounded-md border border-accent/30 bg-accent/5 p-3">
          <p className="text-[11px] uppercase tracking-wider text-muted-foreground">
            Headline
          </p>
          <p className="mt-1 text-sm leading-relaxed">
            {data.headline?.trim() || data.headline_fallback}
          </p>
        </div>

        {data.active_goals.length > 0 ? (
          <div className="space-y-2">
            <p className="text-[11px] uppercase tracking-wider text-muted-foreground">
              Active goals
            </p>
            {data.active_goals.slice(0, 2).map((g) => (
              <ActiveGoalRow key={g.id} goal={g} />
            ))}
            {data.active_goals.length > 2 ? (
              <Link
                to="/goals"
                className="inline-block text-xs text-muted-foreground underline-offset-2 hover:underline"
              >
                +{data.active_goals.length - 2} more
              </Link>
            ) : null}
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}

function BaselineCard({ data }: { data: Zone1Response }) {
  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="font-serif text-base">Still building your baseline</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 text-sm">
        <p className="text-muted-foreground">
          Patterns need about {data.baseline_days_required} days of data
          before they're meaningful. Keep checking in — drainers and chargers
          show up here once we've seen enough to trust them.
        </p>
        {data.mood.length > 0 ? (
          <div>
            <p className="mb-1 text-[11px] uppercase tracking-wider text-muted-foreground">
              Mood so far
            </p>
            <Sparkline points={data.mood} />
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}

function ActiveGoalRow({ goal }: { goal: Zone1GoalStatus }) {
  const ratio = goal.answered_count > 0
    ? `${goal.kept_count}/${goal.answered_count}`
    : "—";
  return (
    <Link
      to="/goals"
      className="block rounded-md border border-border/60 px-3 py-2 transition-colors hover:bg-secondary/50"
    >
      <div className="flex items-baseline justify-between gap-3">
        <span className="truncate text-sm font-medium">{goal.title}</span>
        <span className="shrink-0 text-xs text-muted-foreground">
          Day {goal.day_index} of {goal.total_days}
        </span>
      </div>
      <p className="text-[11px] text-muted-foreground">
        Kept {ratio} so far · ends {goal.end_date}
      </p>
    </Link>
  );
}

function Sparkline({ points }: { points: DailyMoodPoint[] }) {
  // Pure SVG line, no library dependency. Signed -2..+2 scale; pad x by 0.5
  // segments on each side so a short series doesn't hug the edges.
  if (points.length === 0) {
    return (
      <p className="text-xs italic text-muted-foreground">
        No mood logged yet.
      </p>
    );
  }
  const W = 240;
  const H = 48;
  const padX = 4;
  const padY = 4;
  const xs = points.map((_, i) =>
    points.length === 1
      ? W / 2
      : padX + ((W - 2 * padX) * i) / (points.length - 1),
  );
  const ys = points.map((p) => {
    const t = (p.score + 2) / 4; // -2..+2 → 0..1 (0 lands on the midline)
    return H - padY - t * (H - 2 * padY);
  });
  const path = points
    .map((_, i) => (i === 0 ? `M${xs[i]},${ys[i]}` : `L${xs[i]},${ys[i]}`))
    .join(" ");
  return (
    <svg
      viewBox={`0 0 ${W} ${H}`}
      className="mt-1 h-12 w-full max-w-md text-accent"
      preserveAspectRatio="none"
      role="img"
      aria-label={`Mood — ${points.length} days`}
    >
      {/* baseline neutral row */}
      <line
        x1={padX}
        x2={W - padX}
        y1={H / 2}
        y2={H / 2}
        stroke="currentColor"
        strokeOpacity={0.15}
        strokeDasharray="2 3"
      />
      <path
        d={path}
        fill="none"
        stroke="currentColor"
        strokeWidth={1.5}
        strokeLinejoin="round"
        strokeLinecap="round"
      />
      {points.map((_, i) => (
        <circle key={i} cx={xs[i]} cy={ys[i]} r={1.5} fill="currentColor" />
      ))}
    </svg>
  );
}

function computeMoodDelta(
  current: number | null,
  prior: number | null,
): { direction: "up" | "down" | "flat"; label: string } | null {
  if (current === null || prior === null) return null;
  const diff = current - prior;
  if (Math.abs(diff) < 0.05) {
    return { direction: "flat", label: "flat vs last week" };
  }
  const sign = diff > 0 ? "+" : "";
  return {
    direction: diff > 0 ? "up" : "down",
    label: `${sign}${diff.toFixed(1)} vs last week`,
  };
}

function fmtMood(score: number | null): string {
  if (score === null) return "—";
  // Signed scale: make the sign explicit so 0=neutral reads naturally.
  const s = score.toFixed(1);
  return score > 0 ? `+${s}` : s;
}

// ---------- Zone 2 ----------

export function Zone2Card({ data }: { data: Zone2Response }) {
  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="font-serif text-base">
          What's driving it
        </CardTitle>
        <p className="text-xs text-muted-foreground">
          Last {data.window_days} days. Tags with under{" "}
          {LOW_CONFIDENCE_THRESHOLD} appearances are flagged as low confidence.
        </p>
      </CardHeader>
      <CardContent className="grid gap-6 md:grid-cols-2">
        <TagTable
          title="Drainers"
          tone="negative"
          rows={data.drainers}
          empty="Nothing has shown up as a drainer yet."
        />
        <TagTable
          title="Chargers"
          tone="positive"
          rows={data.chargers}
          empty="Nothing has shown up as a charger yet."
        />
      </CardContent>
    </Card>
  );
}

function TagTable({
  title,
  tone,
  rows,
  empty,
}: {
  title: string;
  tone: "positive" | "negative";
  rows: TagAggregate[];
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
          {rows.map((row) => (
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
              <span className="shrink-0 font-mono text-xs tabular-nums text-muted-foreground">
                {row.appearances}d · mood{" "}
                {row.avg_mood !== null ? row.avg_mood.toFixed(1) : "—"}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// ---------- Zone 3 ----------

export function Zone3Card({ data }: { data: Zone3Response }) {
  if (data.goals.length === 0) {
    return (
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="font-serif text-base">What I tried</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3 text-sm text-muted-foreground">
          <p>
            No goals yet. Spot a pattern in your week, then commit to one small
            change — the ledger of what worked (and what didn't) lives here.
          </p>
          <Link
            to="/goals"
            className="inline-flex items-center gap-1 text-accent underline-offset-2 hover:underline"
          >
            <Target className="h-3.5 w-3.5" />
            Shape a goal
          </Link>
        </CardContent>
      </Card>
    );
  }
  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="font-serif text-base">What I tried</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        {data.goals.map((g) => (
          <GoalLedgerRow key={g.id} goal={g} />
        ))}
      </CardContent>
    </Card>
  );
}

function GoalLedgerRow({ goal }: { goal: Goal }) {
  // Label rule (matches GoalsPage::OutcomePill):
  //   status='active'    → "active"
  //   status='abandoned' → "abandoned" (outcome on abandoned rows is
  //                        always 'dropped' under the hood, but we
  //                        prefer status here because "abandoned"
  //                        carries the mid-stream-failure meaning the
  //                        spec uses)
  //   status='completed' → outcome (kept | dropped | inconclusive)
  const label =
    goal.status === "active"
      ? "active"
      : goal.status === "abandoned"
        ? "abandoned"
        : goal.outcome ?? goal.status;
  const tone = (() => {
    if (goal.status === "abandoned") return "border-destructive/40 text-destructive";
    if (goal.outcome === "kept") return "border-accent/40 text-accent";
    if (goal.outcome === "dropped") return "border-destructive/40 text-destructive";
    if (goal.outcome === "inconclusive") return "border-border text-muted-foreground";
    return "border-border text-muted-foreground";
  })();
  return (
    <div className="rounded-md border border-border/60 px-3 py-2">
      <div className="flex items-baseline justify-between gap-3">
        <span className="min-w-0 truncate text-sm font-medium">{goal.title}</span>
        <span
          className={cn(
            "inline-flex shrink-0 items-center rounded-full border px-2 py-0.5 text-[10px] uppercase tracking-wider",
            tone,
          )}
        >
          {label}
        </span>
      </div>
      <p className="text-[11px] text-muted-foreground">
        {goal.start_date} → {goal.end_date}
      </p>
      {goal.conclusion_text ? (
        <p className="mt-1 text-xs text-muted-foreground">{goal.conclusion_text}</p>
      ) : null}
    </div>
  );
}
