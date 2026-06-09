import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Compass } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Textarea } from "@/components/ui/textarea";
import { toast } from "@/components/ui/sonner";

import { postSystemEvent } from "../api";
import { useSetMonthlyIntention, weeklyChatKey } from "../hooks";
import type { ProposalDecision } from "../proposalDecisions";

// Tool args emitted by the model when calling propose_intention. Field
// names match backend/internal/llm/chat/tools.go::ToolProposeIntention.
export interface ProposeIntentionArgs {
  intention?: string;
  why_matters?: string;
}

interface Props {
  sessionId: string;
  args: ProposeIntentionArgs;
  /** Persisted decision derived from the transcript — survives refresh. */
  decision?: ProposalDecision;
}

// ProposeIntentionCard — the inline confirmation card for the month's
// ONE intention (combined weekly+monthly sessions only). Unlike a goal
// there's no check-in question or schedule: an intention is a direction
// ("Protect my mornings"), not a habit. Accept persists it on the
// monthly_reflections row and appends a system_event so the assistant
// sees the decision on later turns.
export function ProposeIntentionCard({ sessionId, args, decision }: Props) {
  const persisted =
    decision && decision.state !== "open" ? decision.goalTitle : undefined;
  const [text, setText] = useState(persisted ?? args.intention ?? "");
  const initialState: "open" | "saved" | "declined" =
    decision?.state === "accepted"
      ? "saved"
      : decision?.state === "declined"
        ? "declined"
        : "open";
  const [state, setState] = useState<"open" | "saving" | "saved" | "declined">(initialState);

  const save = useSetMonthlyIntention();
  const qc = useQueryClient();
  const whyMatters = (args.why_matters ?? "").trim();

  const onAccept = async () => {
    const intention = text.trim();
    if (!intention) {
      toast.error("The intention needs a few words.");
      return;
    }
    setState("saving");
    try {
      await save.mutateAsync(intention);
      const edited = intention !== (args.intention ?? "").trim();
      try {
        await postSystemEvent(
          sessionId,
          edited ? "user_edited_intention" : "user_accepted_intention",
          { intention_text: intention },
        );
      } catch {
        /* best-effort */
      }
      qc.invalidateQueries({ queryKey: weeklyChatKey });
      setState("saved");
    } catch (err) {
      setState("open");
      toast.error("Couldn't save intention", {
        description: err instanceof Error ? err.message : "try again",
      });
    }
  };

  const onDecline = async () => {
    setState("declined");
    try {
      const proposed = (args.intention ?? text).trim();
      await postSystemEvent(
        sessionId,
        "user_declined_intention",
        proposed ? { intention_text: proposed } : undefined,
      );
    } catch {
      /* best-effort */
    }
    qc.invalidateQueries({ queryKey: weeklyChatKey });
  };

  if (state === "saved") {
    return (
      <Card className="border-emerald-500/40 bg-emerald-500/5">
        <CardContent className="space-y-1 px-4 py-3 text-sm">
          <p className="inline-flex items-center gap-2 font-medium">
            <Compass className="h-4 w-4" />
            Intention for next month: {text.trim()}
          </p>
          {whyMatters ? (
            <p className="text-xs text-muted-foreground">
              <span className="font-medium text-foreground/80">Why it matters: </span>
              {whyMatters}
            </p>
          ) : null}
        </CardContent>
      </Card>
    );
  }

  if (state === "declined") {
    return (
      <Card className="border-border/60 bg-muted/30">
        <CardContent className="px-4 py-3 text-sm text-muted-foreground">
          You passed on this intention.
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className="border-primary/30 bg-primary/5">
      <CardContent className="space-y-3 px-4 py-3 text-sm">
        <p className="inline-flex items-center gap-2 text-xs uppercase tracking-wide text-muted-foreground">
          <Compass className="h-3.5 w-3.5" />
          Your intention for next month
        </p>
        <Textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="A direction, in your own words…"
          maxLength={300}
          rows={2}
          className="min-h-[3rem] text-sm"
        />
        {whyMatters ? (
          <p className="text-xs text-muted-foreground">
            <span className="font-medium text-foreground/80">Why it matters: </span>
            {whyMatters}
          </p>
        ) : null}
        <div className="flex gap-2 pt-1">
          <Button
            size="sm"
            onClick={onAccept}
            disabled={state === "saving" || save.isPending}
          >
            {state === "saving" ? "Saving…" : "Set intention"}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            onClick={onDecline}
            disabled={state === "saving" || save.isPending}
          >
            Not this one
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
