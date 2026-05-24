import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Textarea } from "@/components/ui/textarea";
import { toast } from "@/components/ui/sonner";
import { useCompleteGoal } from "@/features/goals/hooks";

import { weeklyChatKey } from "../hooks";
import type { ProposalDecision } from "../proposalDecisions";

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

export interface ProposeCompleteGoalArgs {
  goal_id?: string;
  outcome?: "kept" | "dropped" | "inconclusive";
  reason?: string;
}

interface Props {
  sessionId: string;
  args: ProposeCompleteGoalArgs;
  goalTitle?: string;
  /** Persisted decision derived from the transcript. Drives the
   *  initial state so the card survives a page refresh. */
  decision?: ProposalDecision;
}

// ProposeCompleteGoalCard: inline confirmation for marking an ending
// goal complete. The model proposes an outcome + reason from the user's
// own words; the user can edit before saving.
export function ProposeCompleteGoalCard({ sessionId, args, goalTitle, decision }: Props) {
  // Prefer the persisted outcome over the model's proposal — the user
  // may have flipped it before saving.
  const persistedOutcome =
    decision?.state === "accepted" &&
    (decision.outcome === "kept" || decision.outcome === "dropped" || decision.outcome === "inconclusive")
      ? decision.outcome
      : null;
  const [outcome, setOutcome] = useState<"kept" | "dropped" | "inconclusive">(
    persistedOutcome ?? args.outcome ?? "kept",
  );
  const [reason, setReason] = useState(args.reason ?? "");
  const initialState: "open" | "saved" | "declined" =
    decision?.state === "accepted"
      ? "saved"
      : decision?.state === "declined"
        ? "declined"
        : "open";
  const [state, setState] = useState<"open" | "saving" | "saved" | "declined">(initialState);
  const complete = useCompleteGoal();
  const qc = useQueryClient();

  const pending = state === "saving" || complete.isPending;
  const goalID = args.goal_id ?? "";

  const onAccept = async () => {
    if (!goalID) {
      toast.error("Missing goal reference.");
      return;
    }
    setState("saving");
    try {
      await complete.mutateAsync({
        id: goalID,
        outcome,
        conclusionText: reason.trim(),
      });
      try {
        await postSystemEvent(sessionId, "user_accepted_complete_goal", {
          goal_id: goalID,
          ...(goalTitle ? { goal_title: goalTitle } : {}),
          outcome,
        });
      } catch {
        /* best-effort */
      }
      qc.invalidateQueries({ queryKey: weeklyChatKey });
      setState("saved");
    } catch (err) {
      setState("open");
      toast.error("Couldn't complete goal", {
        description: err instanceof Error ? err.message : "try again",
      });
    }
  };

  const onDecline = async () => {
    setState("declined");
    try {
      await postSystemEvent(sessionId, "user_declined_complete_goal", {
        goal_id: goalID,
        ...(goalTitle ? { goal_title: goalTitle } : {}),
      });
    } catch {
      /* best-effort */
    }
    qc.invalidateQueries({ queryKey: weeklyChatKey });
  };

  if (state === "saved") {
    return (
      <Card className="border-emerald-500/40 bg-emerald-500/5">
        <CardContent className="px-4 py-3 text-sm">
          Marked {goalTitle ? `“${goalTitle}”` : "this goal"} as <b>{outcome}</b>.
        </CardContent>
      </Card>
    );
  }

  if (state === "declined") {
    return (
      <Card className="border-border/60 bg-muted/30">
        <CardContent className="px-4 py-3 text-sm text-muted-foreground">
          You passed on closing out {goalTitle ? `“${goalTitle}”` : "this goal"}.
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className="border-primary/30 bg-primary/5">
      <CardContent className="space-y-3 px-4 py-3 text-sm">
        <p className="text-xs uppercase tracking-wide text-muted-foreground">
          Close out goal
        </p>
        {goalTitle ? (
          <p className="font-medium">{goalTitle}</p>
        ) : (
          <p className="text-muted-foreground">Goal reference: {goalID || "(missing)"}</p>
        )}
        <div className="space-y-1">
          <Label className="text-xs">How did it go?</Label>
          <div className="flex flex-wrap gap-2">
            {(["kept", "dropped", "inconclusive"] as const).map((opt) => (
              <Button
                key={opt}
                type="button"
                size="sm"
                variant={outcome === opt ? "default" : "outline"}
                onClick={() => setOutcome(opt)}
                disabled={pending}
              >
                {opt}
              </Button>
            ))}
          </div>
        </div>
        <div className="space-y-1">
          <Label htmlFor="complete-reason" className="text-xs">
            In your own words
          </Label>
          <Textarea
            id="complete-reason"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder="how it actually went…"
            maxLength={1000}
            className="min-h-[3rem]"
          />
        </div>
        <div className="flex gap-2 pt-1">
          <Button size="sm" onClick={onAccept} disabled={pending}>
            {pending ? "Saving…" : "Save"}
          </Button>
          <Button size="sm" variant="ghost" onClick={onDecline} disabled={pending}>
            Not yet
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
