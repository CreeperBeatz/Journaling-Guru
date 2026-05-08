import { useState } from "react";
import { Plus } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

import type { Goal } from "./api";
import {
  useAbandonGoal,
  useAllGoals,
  useCompleteGoal,
  useCreateGoal,
} from "./hooks";

// GoalsPage — list (active + historical) + create form. The SMART
// shaper modal (Phase 5) will replace the manual form once wired.
export function GoalsPage() {
  const goals = useAllGoals();
  const [creating, setCreating] = useState(false);

  if (goals.isPending) return <p className="text-sm text-muted-foreground">Loading…</p>;
  if (goals.isError) return <p className="text-sm text-destructive">Couldn't load goals.</p>;
  const all = goals.data?.goals ?? [];
  const active = all.filter((g) => g.status === "active");
  const historical = all.filter((g) => g.status !== "active");

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-baseline justify-between gap-3">
        <div className="space-y-1">
          <p className="text-xs uppercase tracking-wider text-muted-foreground">
            Goals
          </p>
          <h1 className="font-serif text-h1">What you're trying</h1>
        </div>
        <Button onClick={() => setCreating(true)} className="gap-1.5">
          <Plus className="h-4 w-4" />
          New goal
        </Button>
      </header>

      {creating ? (
        <CreateGoalCard onClose={() => setCreating(false)} />
      ) : null}

      <section className="space-y-3">
        <h2 className="text-sm font-medium text-muted-foreground">Active</h2>
        {active.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No active goals. Spot a pattern in your week, then commit to one
            small change.
          </p>
        ) : (
          <div className="space-y-3">
            {active.map((g) => (
              <ActiveGoalCard key={g.id} goal={g} />
            ))}
          </div>
        )}
      </section>

      {historical.length > 0 ? (
        <section className="space-y-3">
          <h2 className="text-sm font-medium text-muted-foreground">Past goals</h2>
          <div className="space-y-3">
            {historical.map((g) => (
              <HistoricalGoalCard key={g.id} goal={g} />
            ))}
          </div>
        </section>
      ) : null}
    </div>
  );
}

// ---------------- Active row ----------------

function ActiveGoalCard({ goal }: { goal: Goal }) {
  const complete = useCompleteGoal();
  const abandon = useAbandonGoal();
  const [wrapping, setWrapping] = useState(false);
  const [outcome, setOutcome] = useState<"kept" | "dropped" | "inconclusive">("kept");
  const [conclusionText, setConclusionText] = useState("");

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="font-serif text-base">{goal.title}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <p className="text-sm">{goal.check_in_question}</p>
        <p className="text-xs text-muted-foreground">
          {goal.start_date} → {goal.end_date}
        </p>

        {!wrapping ? (
          <div className="flex flex-wrap gap-2">
            <Button size="sm" onClick={() => setWrapping(true)}>
              Wrap up
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={() =>
                abandon.mutate({ id: goal.id, conclusionText: "" })
              }
              disabled={abandon.isPending}
            >
              Abandon
            </Button>
          </div>
        ) : (
          <div className="space-y-3 rounded-md border border-border/60 p-3">
            <div className="space-y-1">
              <label className="text-xs uppercase tracking-wider text-muted-foreground">
                Outcome
              </label>
              <Select value={outcome} onValueChange={(v) => setOutcome(v as typeof outcome)}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="kept">Kept the change</SelectItem>
                  <SelectItem value="dropped">Dropped it</SelectItem>
                  <SelectItem value="inconclusive">Inconclusive</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-1">
              <label className="text-xs uppercase tracking-wider text-muted-foreground">
                Why? (optional)
              </label>
              <Textarea
                rows={2}
                value={conclusionText}
                onChange={(e) => setConclusionText(e.target.value)}
                maxLength={1000}
                placeholder="What got in the way, what worked…"
              />
            </div>
            <div className="flex gap-2">
              <Button
                size="sm"
                onClick={() => {
                  complete.mutate(
                    { id: goal.id, outcome, conclusionText },
                    { onSuccess: () => setWrapping(false) },
                  );
                }}
                disabled={complete.isPending}
              >
                Save wrap-up
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() => setWrapping(false)}
                disabled={complete.isPending}
              >
                Cancel
              </Button>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

// ---------------- Historical row ----------------

function HistoricalGoalCard({ goal }: { goal: Goal }) {
  return (
    <Card className="bg-secondary/30">
      <CardContent className="space-y-2 pt-4">
        <div className="flex flex-wrap items-baseline justify-between gap-3">
          <span className="font-serif text-base">{goal.title}</span>
          <OutcomePill goal={goal} />
        </div>
        <p className="text-xs text-muted-foreground">
          {goal.start_date} → {goal.end_date}
        </p>
        {goal.conclusion_text ? (
          <p className="text-sm text-muted-foreground">{goal.conclusion_text}</p>
        ) : null}
      </CardContent>
    </Card>
  );
}

function OutcomePill({ goal }: { goal: Goal }) {
  const label =
    goal.status === "abandoned"
      ? "abandoned"
      : goal.outcome ?? goal.status;
  const tone = (() => {
    if (goal.outcome === "kept") return "border-accent text-accent";
    if (goal.outcome === "dropped" || goal.status === "abandoned")
      return "border-destructive/60 text-destructive";
    return "border-border text-muted-foreground";
  })();
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full border px-2 py-0.5 text-xs capitalize",
        tone,
      )}
    >
      {label}
    </span>
  );
}

// ---------------- Create form ----------------

function CreateGoalCard({ onClose }: { onClose: () => void }) {
  const [title, setTitle] = useState("");
  const [question, setQuestion] = useState("");
  const [weeks, setWeeks] = useState(2);
  const create = useCreateGoal();

  const endDate = (() => {
    const d = new Date();
    d.setDate(d.getDate() + weeks * 7);
    const y = d.getFullYear();
    const m = String(d.getMonth() + 1).padStart(2, "0");
    const day = String(d.getDate()).padStart(2, "0");
    return `${y}-${m}-${day}`;
  })();

  const canSubmit =
    title.trim().length > 0 &&
    question.trim().length > 0 &&
    weeks >= 1 &&
    weeks <= 52;

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="font-serif text-base">New goal</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="space-y-1">
          <label className="text-xs uppercase tracking-wider text-muted-foreground">
            Title
          </label>
          <Input
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="e.g. Cut phone use after 22:00"
            maxLength={200}
          />
        </div>
        <div className="space-y-1">
          <label className="text-xs uppercase tracking-wider text-muted-foreground">
            Daily check-in question
          </label>
          <Input
            value={question}
            onChange={(e) => setQuestion(e.target.value)}
            placeholder="e.g. Did you stay under your screen-time limit today?"
            maxLength={200}
          />
          <p className="text-[11px] italic text-muted-foreground">
            Yes/no — phrase it so the answer is clear.
          </p>
        </div>
        <div className="space-y-1">
          <label className="text-xs uppercase tracking-wider text-muted-foreground">
            Run for
          </label>
          <div className="flex items-center gap-2">
            <Input
              type="number"
              min={1}
              max={52}
              value={weeks}
              onChange={(e) => setWeeks(Number(e.target.value) || 1)}
              className="w-20"
            />
            <span className="text-sm text-muted-foreground">
              week{weeks === 1 ? "" : "s"} · ends {endDate}
            </span>
          </div>
        </div>
        <div className="flex gap-2 pt-1">
          <Button
            size="sm"
            disabled={!canSubmit || create.isPending}
            onClick={() =>
              create.mutate(
                {
                  title: title.trim(),
                  check_in_question: question.trim(),
                  end_date: endDate,
                },
                { onSuccess: () => onClose() },
              )
            }
          >
            Create
          </Button>
          <Button size="sm" variant="outline" onClick={onClose} disabled={create.isPending}>
            Cancel
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
