import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { toast } from "@/components/ui/sonner";
import { useCreateGoal } from "@/features/goals/hooks";

import { weeklyChatKey } from "../hooks";
import type { ProposalDecision } from "../proposalDecisions";

// Local <Label> shim — the project doesn't ship a shadcn label
// primitive, so use the native element with a stable class.
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
import { usePatchReflection } from "../hooks";

// Tool args emitted by the model when calling propose_goal. Field names
// match backend/internal/llm/chat/tools.go::ToolProposeGoal parameters.
export interface ProposeGoalArgs {
  title?: string;
  check_in_question?: string;
  why_matters?: string;
  if_followed?: string;
  if_not_followed?: string;
  duration_weeks?: number;
}

interface Props {
  sessionId: string;
  args: ProposeGoalArgs;
  /** Persisted decision derived from the transcript by
   *  resolveProposalDecisions. Drives the card's initial state so it
   *  survives a page refresh. Undefined ≡ open. */
  decision?: ProposalDecision;
}

const MOTIVATION_MAX = 600;

// ProposeGoalCard renders the inline confirmation card for a model-
// proposed weekly goal. Every field — title, check-in, weeks, and the
// three "why" fields — is editable and persisted on the goals row when
// the user accepts. The model pre-fills the why fields with the user's
// verbatim words from the chat; the user can polish or shorten them
// before saving.
//
// Lifecycle:
//   1. Initial: form prefilled with the model's proposal.
//   2. Accept: createGoal → patchReflection({new_goal_id}) → switch to
//      saved state, append user_accepted_goal system_event.
//   3. Decline: append user_declined_goal system_event, switch to
//      declined state.
//
// Decision is one-shot per card render. A future turn proposing a
// different goal will mount a fresh card.
export function ProposeGoalCard({ sessionId, args, decision }: Props) {
  // Prefer the persisted decision title — it's what the user actually
  // saved (post-edit), versus args.title which is the model's proposal.
  const persistedTitle =
    decision && decision.state !== "open" ? decision.goalTitle : undefined;
  const initialTitle = persistedTitle ?? args.title ?? "";
  const [title, setTitle] = useState(initialTitle);
  const [checkIn, setCheckIn] = useState(args.check_in_question ?? "");
  const [weeks, setWeeks] = useState(args.duration_weeks ?? 1);
  const [whyMatters, setWhyMatters] = useState(args.why_matters ?? "");
  const [ifFollowed, setIfFollowed] = useState(args.if_followed ?? "");
  const [ifNotFollowed, setIfNotFollowed] = useState(args.if_not_followed ?? "");
  const initialState: "open" | "saved" | "declined" =
    decision?.state === "accepted"
      ? "saved"
      : decision?.state === "declined"
        ? "declined"
        : "open";
  const [state, setState] = useState<"open" | "saving" | "saved" | "declined">(initialState);

  const createGoal = useCreateGoal();
  const patch = usePatchReflection();
  const qc = useQueryClient();

  const pending = state === "saving" || createGoal.isPending;

  const onAccept = async () => {
    if (!title.trim() || !checkIn.trim()) {
      toast.error("Goal needs a title and a check-in question.");
      return;
    }
    setState("saving");
    try {
      const goal = await createGoal.mutateAsync({
        title: title.trim(),
        check_in_question: checkIn.trim(),
        duration_weeks: Math.max(1, Math.min(52, weeks)),
        why_matters: whyMatters.trim(),
        if_followed: ifFollowed.trim(),
        if_not_followed: ifNotFollowed.trim(),
      });
      try {
        await patch.mutateAsync({ new_goal_id: goal.id });
      } catch {
        // Non-fatal: the goal still exists; the reflection ↔ goal
        // link is best-effort.
      }
      try {
        await postSystemEvent(sessionId, "user_accepted_goal", {
          goal_id: goal.id,
          goal_title: title.trim(),
        });
      } catch {
        /* best-effort */
      }
      // Refetch the chat session so the new system_event lands in the
      // messages array — resolveProposalDecisions will then derive the
      // saved state from the transcript on subsequent renders /
      // navigations.
      qc.invalidateQueries({ queryKey: weeklyChatKey });
      setState("saved");
    } catch (err) {
      setState("open");
      toast.error("Couldn't save goal", {
        description: err instanceof Error ? err.message : "try again",
      });
    }
  };

  const onDecline = async () => {
    setState("declined");
    try {
      // No goal_id on decline — nothing was persisted — but a title
      // keeps the LLM's event-line concrete on later turns.
      const proposedTitle = (args.title ?? title).trim();
      await postSystemEvent(sessionId, "user_declined_goal",
        proposedTitle ? { goal_title: proposedTitle } : undefined);
    } catch {
      /* best-effort */
    }
    qc.invalidateQueries({ queryKey: weeklyChatKey });
  };

  if (state === "saved") {
    return (
      <Card className="border-emerald-500/40 bg-emerald-500/5">
        <CardContent className="space-y-2 px-4 py-3 text-sm">
          <p className="font-medium">Goal saved: {title.trim()}</p>
          <p className="text-muted-foreground">
            Daily check-in: {checkIn.trim()}
          </p>
          {(whyMatters.trim() || ifFollowed.trim() || ifNotFollowed.trim()) && (
            <div className="space-y-1 pt-1 text-xs text-muted-foreground">
              {whyMatters.trim() ? (
                <p>
                  <span className="font-medium text-foreground/80">Why it matters: </span>
                  {whyMatters.trim()}
                </p>
              ) : null}
              {ifFollowed.trim() ? (
                <p>
                  <span className="font-medium text-foreground/80">If I follow it: </span>
                  {ifFollowed.trim()}
                </p>
              ) : null}
              {ifNotFollowed.trim() ? (
                <p>
                  <span className="font-medium text-foreground/80">If I don&apos;t: </span>
                  {ifNotFollowed.trim()}
                </p>
              ) : null}
            </div>
          )}
        </CardContent>
      </Card>
    );
  }

  if (state === "declined") {
    return (
      <Card className="border-border/60 bg-muted/30">
        <CardContent className="px-4 py-3 text-sm text-muted-foreground">
          You passed on this goal.
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className="border-primary/30 bg-primary/5">
      <CardContent className="space-y-3 px-4 py-3 text-sm">
        <p className="text-xs uppercase tracking-wide text-muted-foreground">
          Proposed goal for next week
        </p>
        <div className="space-y-1">
          <Label htmlFor="goal-title" className="text-xs">
            Title
          </Label>
          <Input
            id="goal-title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            placeholder="What you'll try"
            maxLength={120}
          />
        </div>
        <div className="space-y-1">
          <Label htmlFor="goal-checkin" className="text-xs">
            Daily check-in question
          </Label>
          <Input
            id="goal-checkin"
            value={checkIn}
            onChange={(e) => setCheckIn(e.target.value)}
            placeholder="Did you do it today?"
            maxLength={160}
          />
        </div>
        <div className="space-y-1">
          <Label htmlFor="goal-weeks" className="text-xs">
            How many weeks
          </Label>
          <Input
            id="goal-weeks"
            type="number"
            min={1}
            max={12}
            value={weeks}
            onChange={(e) => setWeeks(Number(e.target.value) || 1)}
            className="w-24"
          />
        </div>

        <div className="space-y-3 rounded-lg border border-border/60 bg-background/60 px-3 py-3">
          <div className="space-y-1">
            <Label htmlFor="goal-why" className="text-xs">
              Why it matters to you
            </Label>
            <Textarea
              id="goal-why"
              value={whyMatters}
              onChange={(e) => setWhyMatters(e.target.value)}
              placeholder="In your own words…"
              maxLength={MOTIVATION_MAX}
              rows={2}
              className="min-h-[3rem] text-sm"
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="goal-if-followed" className="text-xs">
              What happens if you follow it
            </Label>
            <Textarea
              id="goal-if-followed"
              value={ifFollowed}
              onChange={(e) => setIfFollowed(e.target.value)}
              placeholder="The payoff you're aiming at…"
              maxLength={MOTIVATION_MAX}
              rows={2}
              className="min-h-[3rem] text-sm"
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="goal-if-not-followed" className="text-xs">
              What happens if you don&apos;t
            </Label>
            <Textarea
              id="goal-if-not-followed"
              value={ifNotFollowed}
              onChange={(e) => setIfNotFollowed(e.target.value)}
              placeholder="The cost of skipping it…"
              maxLength={MOTIVATION_MAX}
              rows={2}
              className="min-h-[3rem] text-sm"
            />
          </div>
        </div>

        <div className="flex gap-2 pt-1">
          <Button size="sm" onClick={onAccept} disabled={pending}>
            {pending ? "Saving…" : "Accept"}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            onClick={onDecline}
            disabled={pending}
          >
            Not this one
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
