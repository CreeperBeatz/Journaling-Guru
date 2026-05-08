import { Check, X } from "lucide-react";
import { Link } from "react-router-dom";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";

import { useActiveGoals, useCheckInGoal } from "./hooks";
import type { Goal } from "./api";

// GoalCheckInBlock — yes/no per active goal, rendered at the bottom of
// /today's Manual tab and at the end of the chat flow. Hidden when the
// user has no active goals (no point in an empty card).
//
// Each row pre-fills the user's existing answer from
// active_goals.todays_check_ins; clicking either pill upserts via
// POST /api/goals/:id/check-ins.
export function GoalCheckInBlock() {
  const goals = useActiveGoals();
  const checkIn = useCheckInGoal();

  if (goals.isPending) return null;
  if (goals.isError) return null; // silent — non-critical for the daily flow
  const items = goals.data?.goals ?? [];
  if (items.length === 0) return null;

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="font-serif text-base">Active goals</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        {items.map((g) => (
          <GoalRow
            key={g.id}
            goal={g}
            currentValue={goals.data?.todays_check_ins?.[g.id]}
            onAnswer={(v) => checkIn.mutate({ goalId: g.id, value: v })}
            disabled={checkIn.isPending}
          />
        ))}
      </CardContent>
    </Card>
  );
}

function GoalRow({
  goal,
  currentValue,
  onAnswer,
  disabled,
}: {
  goal: Goal;
  currentValue: boolean | undefined;
  onAnswer: (v: boolean) => void;
  disabled: boolean;
}) {
  const yesActive = currentValue === true;
  const noActive = currentValue === false;
  const answered = currentValue !== undefined;

  return (
    <div className="flex items-start justify-between gap-3 rounded-md border border-border/60 p-3">
      <div className="min-w-0 flex-1">
        <Link
          to="/goals"
          className="block truncate text-sm font-medium hover:underline"
        >
          {goal.title}
        </Link>
        <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">
          {goal.check_in_question}
        </p>
        {!answered ? (
          <p className="mt-1 text-[11px] italic text-muted-foreground">
            Not answered today
          </p>
        ) : null}
      </div>
      <div className="flex shrink-0 items-center gap-1">
        <button
          type="button"
          onClick={() => onAnswer(true)}
          disabled={disabled}
          aria-pressed={yesActive}
          aria-label="Yes"
          className={cn(
            "flex h-9 w-9 items-center justify-center rounded-full border transition-colors",
            yesActive
              ? "border-accent bg-accent/15 text-accent"
              : "border-border bg-card hover:bg-secondary",
          )}
        >
          <Check className="h-4 w-4" />
        </button>
        <button
          type="button"
          onClick={() => onAnswer(false)}
          disabled={disabled}
          aria-pressed={noActive}
          aria-label="No"
          className={cn(
            "flex h-9 w-9 items-center justify-center rounded-full border transition-colors",
            noActive
              ? "border-destructive bg-destructive/10 text-destructive"
              : "border-border bg-card hover:bg-secondary",
          )}
        >
          <X className="h-4 w-4" />
        </button>
      </div>
    </div>
  );
}
