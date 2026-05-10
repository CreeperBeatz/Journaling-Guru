import { useMemo, useState } from "react";
import { Check, Plus } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Textarea } from "@/components/ui/textarea";
import { toast } from "@/components/ui/sonner";
import { cn } from "@/lib/utils";

import type { Zone1GoalStatus } from "@/features/summary/api";
import { useCompleteGoal, useExtendGoal } from "@/features/goals/hooks";

import { ReflectionResponse } from "../api";
import { usePatchReflection } from "../hooks";

interface Props {
  data: ReflectionResponse;
  onContinue: () => void;
  saving: boolean;
}

type Outcome = "kept" | "dropped" | "inconclusive";
type EndingResolution =
  | { kind: "extend"; weeks: number }
  | { kind: "complete"; outcome: Outcome; reason: string };

// Card 2 — review every active goal. Goals split into two buckets:
//
//   - Ending this reflection (end_date <= week_end): user picks Extend
//     (1/2/4 weeks) or Complete (with outcome + free-text reason).
//   - Mid-flight (end_date > week_end): optional "how's it going so
//     far?" textarea persisted to weekly_reflections.goal_notes.
//
// Continue is disabled until every ending-bucket goal is resolved.
// Abandon is intentionally NOT here — it lives on the goals side menu.
export function GoalReviewCard({ data, onContinue, saving }: Props) {
  const { ending, midFlight } = useMemo(
    () => splitGoalsByEndDate(data.active_goals, data.week_end),
    [data.active_goals, data.week_end],
  );

  // Each ending-bucket goal is resolved into either an extend or a
  // complete + reason. Empty until the user clicks something.
  const [resolutions, setResolutions] = useState<Record<string, EndingResolution>>({});
  const allResolved = ending.every((g) => resolutions[g.id] != null);

  const extend = useExtendGoal();
  const complete = useCompleteGoal();

  const submitResolutions = async () => {
    for (const goal of ending) {
      const res = resolutions[goal.id];
      if (!res) continue;
      try {
        if (res.kind === "extend") {
          await extend.mutateAsync({ id: goal.id, weeks: res.weeks });
        } else {
          await complete.mutateAsync({
            id: goal.id,
            outcome: res.outcome,
            conclusionText: res.reason,
          });
        }
      } catch {
        // Toasts handled by hooks; bail on first failure so the user
        // can fix it.
        toast.error("Couldn't save your goal updates", {
          description: "Try again, or skip with the side menu.",
        });
        return;
      }
    }
    onContinue();
  };

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <h2 className="font-serif text-h2">Your active goals</h2>
        <p className="text-sm text-muted-foreground">
          Review what's running. Goals always end on your reflection day —
          extend, complete, or just check in.
        </p>
      </header>

      {ending.length === 0 && midFlight.length === 0 ? (
        <Card>
          <CardContent className="px-6 py-8 text-sm text-muted-foreground">
            No active goals right now. Continue to shape one if you'd like.
          </CardContent>
        </Card>
      ) : null}

      {ending.length > 0 ? (
        <section className="space-y-3">
          <h3 className="font-mono text-[11px] uppercase tracking-wider text-muted-foreground">
            Ending this reflection
          </h3>
          {ending.map((g) => (
            <EndingGoalRow
              key={g.id}
              goal={g}
              resolution={resolutions[g.id]}
              onResolve={(r) =>
                setResolutions((prev) => ({ ...prev, [g.id]: r }))
              }
              onClear={() =>
                setResolutions((prev) => {
                  const next = { ...prev };
                  delete next[g.id];
                  return next;
                })
              }
            />
          ))}
        </section>
      ) : null}

      {midFlight.length > 0 ? (
        <section className="space-y-3">
          <h3 className="font-mono text-[11px] uppercase tracking-wider text-muted-foreground">
            Still in flight
          </h3>
          {midFlight.map((g) => (
            <MidFlightGoalRow
              key={g.id}
              goal={g}
              note={data.goal_notes[g.id] ?? ""}
            />
          ))}
        </section>
      ) : null}

      <div className="flex items-center justify-between gap-3">
        <p className="text-xs italic text-muted-foreground">
          {ending.length > 0 && !allResolved
            ? "Resolve each ending goal — extend or complete — to continue."
            : null}
        </p>
        <Button
          onClick={submitResolutions}
          disabled={
            saving ||
            extend.isPending ||
            complete.isPending ||
            !allResolved
          }
        >
          Continue
        </Button>
      </div>
    </div>
  );
}

function EndingGoalRow({
  goal,
  resolution,
  onResolve,
  onClear,
}: {
  goal: Zone1GoalStatus;
  resolution: EndingResolution | undefined;
  onResolve: (r: EndingResolution) => void;
  onClear: () => void;
}) {
  const [mode, setMode] = useState<"idle" | "extend" | "complete">(() =>
    resolution?.kind ?? "idle",
  );

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="font-serif text-sm">{goal.title}</CardTitle>
        <p className="text-[11px] text-muted-foreground">
          Day {goal.day_index} of {goal.total_days} · {goal.kept_count}/
          {goal.answered_count || 7} kept · ends {goal.end_date}
        </p>
      </CardHeader>
      <CardContent className="space-y-3">
        {resolution ? (
          <ResolvedBadge resolution={resolution} onClear={() => {
            setMode("idle");
            onClear();
          }} />
        ) : null}

        {!resolution && mode === "idle" ? (
          <div className="flex flex-wrap gap-2">
            <Button size="sm" variant="outline" onClick={() => setMode("extend")}>
              Extend
            </Button>
            <Button size="sm" onClick={() => setMode("complete")}>
              Complete
            </Button>
          </div>
        ) : null}

        {!resolution && mode === "extend" ? (
          <ExtendChooser
            onCancel={() => setMode("idle")}
            onPick={(weeks) => {
              onResolve({ kind: "extend", weeks });
            }}
          />
        ) : null}

        {!resolution && mode === "complete" ? (
          <CompleteForm
            onCancel={() => setMode("idle")}
            onSubmit={(outcome, reason) => {
              onResolve({ kind: "complete", outcome, reason });
            }}
          />
        ) : null}
      </CardContent>
    </Card>
  );
}

function ResolvedBadge({
  resolution,
  onClear,
}: {
  resolution: EndingResolution;
  onClear: () => void;
}) {
  const label =
    resolution.kind === "extend"
      ? `Will extend ${resolution.weeks} week${resolution.weeks === 1 ? "" : "s"}`
      : `Will complete: ${resolution.outcome}`;
  return (
    <div className="flex items-center justify-between gap-2 rounded-md border border-accent/40 bg-accent/10 px-3 py-2 text-sm">
      <span className="inline-flex items-center gap-2">
        <Check className="h-4 w-4 text-accent" />
        {label}
      </span>
      <button
        type="button"
        onClick={onClear}
        className="text-xs text-muted-foreground underline-offset-2 hover:underline"
      >
        Change
      </button>
    </div>
  );
}

function ExtendChooser({
  onPick,
  onCancel,
}: {
  onPick: (weeks: number) => void;
  onCancel: () => void;
}) {
  return (
    <div className="space-y-2">
      <p className="text-xs text-muted-foreground">Extend by:</p>
      <div className="flex flex-wrap gap-2">
        {[1, 2, 4].map((w) => (
          <Button
            key={w}
            size="sm"
            variant="outline"
            onClick={() => onPick(w)}
            className="gap-1"
          >
            <Plus className="h-3 w-3" />
            {w} week{w === 1 ? "" : "s"}
          </Button>
        ))}
        <button
          type="button"
          onClick={onCancel}
          className="text-xs text-muted-foreground underline-offset-2 hover:underline"
        >
          Cancel
        </button>
      </div>
    </div>
  );
}

function CompleteForm({
  onSubmit,
  onCancel,
}: {
  onSubmit: (outcome: Outcome, reason: string) => void;
  onCancel: () => void;
}) {
  const [outcome, setOutcome] = useState<Outcome | null>(null);
  const [reason, setReason] = useState("");
  const canSubmit = outcome !== null && reason.trim().length > 0;

  return (
    <div className="space-y-2">
      <p className="text-xs text-muted-foreground">How did it go?</p>
      <div className="flex flex-wrap gap-2">
        {(["kept", "dropped", "inconclusive"] as Outcome[]).map((o) => (
          <button
            key={o}
            type="button"
            onClick={() => setOutcome(o)}
            className={cn(
              "rounded-full border px-3 py-1 text-xs capitalize transition-colors",
              outcome === o
                ? "border-accent bg-accent/20 text-foreground"
                : "border-border/60 hover:bg-muted/40",
            )}
          >
            {o}
          </button>
        ))}
      </div>
      <Textarea
        value={reason}
        onChange={(e) => setReason(e.target.value)}
        rows={2}
        maxLength={1000}
        placeholder="What happened? (required)"
        className="text-sm"
      />
      <div className="flex flex-wrap gap-2">
        <Button
          size="sm"
          disabled={!canSubmit}
          onClick={() => outcome && onSubmit(outcome, reason.trim())}
        >
          Mark complete
        </Button>
        <button
          type="button"
          onClick={onCancel}
          className="text-xs text-muted-foreground underline-offset-2 hover:underline"
        >
          Cancel
        </button>
      </div>
    </div>
  );
}

function MidFlightGoalRow({
  goal,
  note,
}: {
  goal: Zone1GoalStatus;
  note: string;
}) {
  const [text, setText] = useState(note);
  const patch = usePatchReflection();

  const flush = () => {
    if (text.trim() === note.trim()) return;
    patch.mutate({ goal_id: goal.id, goal_note: text.trim() });
  };

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="font-serif text-sm">{goal.title}</CardTitle>
        <p className="text-[11px] text-muted-foreground">
          Day {goal.day_index} of {goal.total_days} · {goal.kept_count}/
          {goal.answered_count || 7} kept · ends {goal.end_date}
        </p>
      </CardHeader>
      <CardContent>
        <Textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          onBlur={flush}
          rows={2}
          maxLength={4000}
          placeholder="How's it going so far? (optional)"
          className="border-transparent bg-transparent px-0 text-sm focus-visible:rounded-none focus-visible:border-b focus-visible:border-b-border focus-visible:ring-0 focus-visible:ring-offset-0"
        />
      </CardContent>
    </Card>
  );
}

function splitGoalsByEndDate(
  goals: Zone1GoalStatus[],
  weekEnd: string,
): { ending: Zone1GoalStatus[]; midFlight: Zone1GoalStatus[] } {
  const ending: Zone1GoalStatus[] = [];
  const midFlight: Zone1GoalStatus[] = [];
  for (const g of goals) {
    if (g.end_date <= weekEnd) ending.push(g);
    else midFlight.push(g);
  }
  return { ending, midFlight };
}
