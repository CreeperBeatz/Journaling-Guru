import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { toast } from "@/components/ui/sonner";
import { useExtendGoal } from "@/features/goals/hooks";

function Label({
  htmlFor,
  className,
  children,
}: {
  htmlFor?: string;
  className?: string;
  children: React.ReactNode;
}) {
  return (
    <label
      htmlFor={htmlFor}
      className={`text-xs font-medium text-foreground/85 ${className ?? ""}`}
    >
      {children}
    </label>
  );
}

import { postSystemEvent } from "../api";

export interface ProposeExtendGoalArgs {
  goal_id?: string;
  weeks?: number;
}

interface Props {
  sessionId: string;
  args: ProposeExtendGoalArgs;
  // Looked-up active goal so we can render its title; nullable in case
  // the goal list was stale.
  goalTitle?: string;
}

// ProposeExtendGoalCard: inline confirmation for extending an ending
// goal. Accept hits the existing extendGoal endpoint (PATCH /api/goals/
// :id with action=extend, extend_weeks=N).
export function ProposeExtendGoalCard({ sessionId, args, goalTitle }: Props) {
  const [weeks, setWeeks] = useState(args.weeks ?? 1);
  const [state, setState] = useState<"open" | "saving" | "saved" | "declined">("open");
  const extend = useExtendGoal();

  const pending = state === "saving" || extend.isPending;
  const goalID = args.goal_id ?? "";

  const onAccept = async () => {
    if (!goalID) {
      toast.error("Missing goal reference.");
      return;
    }
    setState("saving");
    const clampedWeeks = Math.max(1, Math.min(12, weeks));
    try {
      await extend.mutateAsync({ id: goalID, weeks: clampedWeeks });
      try {
        await postSystemEvent(sessionId, "user_accepted_extend_goal", {
          goal_id: goalID,
          ...(goalTitle ? { goal_title: goalTitle } : {}),
          weeks: String(clampedWeeks),
        });
      } catch {
        /* best-effort */
      }
      setState("saved");
    } catch (err) {
      setState("open");
      toast.error("Couldn't extend goal", {
        description: err instanceof Error ? err.message : "try again",
      });
    }
  };

  const onDecline = async () => {
    setState("declined");
    try {
      await postSystemEvent(sessionId, "user_declined_extend_goal", {
        goal_id: goalID,
        ...(goalTitle ? { goal_title: goalTitle } : {}),
      });
    } catch {
      /* best-effort */
    }
  };

  if (state === "saved") {
    return (
      <Card className="border-emerald-500/40 bg-emerald-500/5">
        <CardContent className="px-4 py-3 text-sm">
          Extended {goalTitle ? `“${goalTitle}”` : "this goal"} for {weeks} more
          week{weeks > 1 ? "s" : ""}.
        </CardContent>
      </Card>
    );
  }

  if (state === "declined") {
    return (
      <Card className="border-border/60 bg-muted/30">
        <CardContent className="px-4 py-3 text-sm text-muted-foreground">
          You passed on extending {goalTitle ? `“${goalTitle}”` : "this goal"}.
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className="border-primary/30 bg-primary/5">
      <CardContent className="space-y-3 px-4 py-3 text-sm">
        <p className="text-xs uppercase tracking-wide text-muted-foreground">
          Extend goal
        </p>
        {goalTitle ? (
          <p className="font-medium">{goalTitle}</p>
        ) : (
          <p className="text-muted-foreground">Goal reference: {goalID || "(missing)"}</p>
        )}
        <div className="space-y-1">
          <Label htmlFor="extend-weeks" className="text-xs">
            How many more weeks
          </Label>
          <Input
            id="extend-weeks"
            type="number"
            min={1}
            max={12}
            value={weeks}
            onChange={(e) => setWeeks(Number(e.target.value) || 1)}
            className="w-24"
          />
        </div>
        <div className="flex gap-2 pt-1">
          <Button size="sm" onClick={onAccept} disabled={pending}>
            {pending ? "Saving…" : "Extend"}
          </Button>
          <Button size="sm" variant="ghost" onClick={onDecline} disabled={pending}>
            Not now
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
