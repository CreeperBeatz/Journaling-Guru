import { useState } from "react";
import { Sparkles } from "lucide-react";

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button, buttonVariants } from "@/components/ui/button";
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
  useActiveGoals,
  useAllGoals,
  useCompleteGoal,
  useCreateGoal,
} from "./hooks";
import { SmartShaperModal } from "./SmartShaperModal";

// GoalsPage — list (active + historical) + chat-first SMART shaper.
// Manual fallback is one click away from inside the shaper modal.
//
// We call useActiveGoals alongside useAllGoals so we have a tz-correct
// "today" string to drive the wrap-up gating in ActiveGoalCard. The
// active query returns `local_date` from the server (resolved against
// the user's timezone + day_start_minutes); deriving today from
// browser-local Date would drift on the day_start boundary.
export function GoalsPage() {
  const goals = useAllGoals();
  const active = useActiveGoals();
  const today = active.data?.local_date ?? null;
  const [creating, setCreating] = useState(false);
  const [shaperOpen, setShaperOpen] = useState(false);

  if (goals.isPending) return <p className="text-sm text-muted-foreground">Loading…</p>;
  if (goals.isError) return <p className="text-sm text-destructive">Couldn't load goals.</p>;
  const all = goals.data?.goals ?? [];
  const activeGoals = all.filter((g) => g.status === "active");
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
        {/* Single primary CTA. The manual create form stays reachable
         *  via the SmartShaperModal's "Skip the shaper" fallback link,
         *  intentionally adding friction for manual entry — the spec
         *  prefers the SMART-shape conversation as the default path. */}
        <Button onClick={() => setShaperOpen(true)} className="gap-1.5">
          <Sparkles className="h-4 w-4" />
          Shape a goal
        </Button>
      </header>

      <SmartShaperModal
        open={shaperOpen}
        onOpenChange={setShaperOpen}
        onFallback={() => setCreating(true)}
      />

      {creating ? (
        <CreateGoalCard onClose={() => setCreating(false)} />
      ) : null}

      <section className="space-y-3">
        <h2 className="text-sm font-medium text-muted-foreground">Active</h2>
        {activeGoals.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No active goals. Spot a pattern in your week, then commit to one
            small change.
          </p>
        ) : (
          <div className="space-y-3">
            {activeGoals.map((g) => (
              <ActiveGoalCard key={g.id} goal={g} today={today} />
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

function ActiveGoalCard({
  goal,
  today,
}: {
  goal: Goal;
  // tz-correct YYYY-MM-DD from useActiveGoals; null while loading.
  today: string | null;
}) {
  const complete = useCompleteGoal();
  const abandon = useAbandonGoal();
  const [outcome, setOutcome] = useState<"kept" | "dropped" | "inconclusive">("kept");
  const [conclusionText, setConclusionText] = useState("");
  const [abandonOpen, setAbandonOpen] = useState(false);
  const [abandonReason, setAbandonReason] = useState("");

  // Spec: "On the end date, the system asks the user to wrap up the
  // goal." Wrap-up is event-triggered, not a manual mid-stream control
  // — so we surface the form automatically when the goal has reached
  // (or passed) its end_date. ISO YYYY-MM-DD strings sort
  // lexicographically, so a string compare is equivalent to a date
  // compare here.
  const dueForWrapUp = today !== null && goal.end_date <= today;

  return (
    <Card className={dueForWrapUp ? "border-accent/50" : undefined}>
      <CardHeader className="pb-2">
        <CardTitle className="font-serif text-base">{goal.title}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <p className="text-sm">{goal.check_in_question}</p>
        <p className="text-xs text-muted-foreground">
          {goal.start_date} → {goal.end_date}
        </p>

        {dueForWrapUp ? (
          <div className="space-y-3 rounded-md border border-accent/40 bg-accent/5 p-3">
            <p className="text-sm font-medium">
              This goal ends today — how did it go?
            </p>
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
            <Button
              size="sm"
              onClick={() => {
                complete.mutate({ id: goal.id, outcome, conclusionText });
              }}
              disabled={complete.isPending}
            >
              Save wrap-up
            </Button>
          </div>
        ) : null}

        {/* Abandon is always available — mid-stream "this isn't working"
         *  is a separate path from the natural end-date wrap-up. Spec:
         *  "Failure data is more valuable than completion data."
         *
         *  The why-modal is required: knowing why a goal didn't stick
         *  is the actual value of capturing the failure, so we won't
         *  let the user submit an empty reason. */}
        <Button
          size="sm"
          variant="outline"
          onClick={() => {
            setAbandonReason("");
            setAbandonOpen(true);
          }}
          disabled={abandon.isPending}
        >
          Abandon
        </Button>
      </CardContent>

      <AlertDialog open={abandonOpen} onOpenChange={setAbandonOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Abandon &ldquo;{goal.title}&rdquo;?</AlertDialogTitle>
            <AlertDialogDescription>
              Knowing why you didn&apos;t follow your goal to the end is more
              important than actually following it. Make sure to fill that in.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <Textarea
            rows={3}
            value={abandonReason}
            onChange={(e) => setAbandonReason(e.target.value)}
            maxLength={1000}
            placeholder="What got in the way? Wrong shape, wrong time, wrong question…"
            autoFocus
          />
          <AlertDialogFooter>
            <AlertDialogCancel>Keep the goal</AlertDialogCancel>
            <AlertDialogAction
              className={cn(buttonVariants({ variant: "destructive" }))}
              disabled={abandonReason.trim().length === 0 || abandon.isPending}
              onClick={(e) => {
                if (abandonReason.trim().length === 0) {
                  // Belt-and-suspenders: AlertDialogAction auto-closes
                  // on click, so we cancel that path when empty even
                  // though `disabled` should already block it.
                  e.preventDefault();
                  return;
                }
                abandon.mutate({
                  id: goal.id,
                  conclusionText: abandonReason.trim(),
                });
              }}
            >
              Abandon goal
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
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
