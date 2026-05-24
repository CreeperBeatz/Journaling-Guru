import { CheckCircle2, MessageCircle, RotateCcw, Sparkles } from "lucide-react";
import { useNavigate } from "react-router-dom";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

import type { Zone1GoalStatus } from "@/features/summary/api";

import { ReflectionResponse } from "../api";
import { useReplayReflection } from "../hooks";
import { WeeklySynthesisCard } from "./WeeklySynthesisCard";

// DonePage — read-only summary of a completed weekly reflection. Shown
// for the rest of the week after the user finishes the wizard, and
// reused by History for past weeks. `allowReplay` defaults true (live
// wizard); History passes false because the replay endpoint is
// this-week scoped.
export function DonePage({
  data,
  completedAt,
  allowReplay = true,
}: {
  data: ReflectionResponse;
  completedAt: string | null;
  allowReplay?: boolean;
}) {
  const completedWhen = completedAt ? new Date(completedAt) : null;
  const replay = useReplayReflection();
  const navigate = useNavigate();

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <p className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground">
          Weekly reflection
        </p>
        <h1 className="font-serif text-h1 inline-flex items-center gap-2">
          <CheckCircle2 className="h-6 w-6 text-accent" />
          You wrapped this week.
        </h1>
        <p className="text-sm text-muted-foreground">
          {data.week_start} → {data.week_end}
          {completedWhen
            ? ` · completed ${completedWhen.toLocaleString()}`
            : ""}
        </p>
      </header>

      {allowReplay ? (
        <Card className="border-primary/30 bg-primary/5">
          <CardContent className="flex flex-col gap-2 px-4 py-3 text-sm sm:flex-row sm:items-center sm:justify-between">
            <p className="text-foreground/85">
              Want to keep talking? Your conversation is still there.
            </p>
            <Button
              size="sm"
              onClick={() => navigate("/weekly/chat")}
              className="gap-2 self-start sm:self-auto"
            >
              <MessageCircle className="h-4 w-4" />
              Continue chat
            </Button>
          </CardContent>
        </Card>
      ) : null}

      <WeeklySynthesisCard data={data} />

      {data.surprise_text.trim().length > 0 ? (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="font-serif text-base">
              The thing to remember
            </CardTitle>
            <p className="text-xs italic text-muted-foreground">
              Distilled from the reflection chat — fed into next week's letter.
            </p>
          </CardHeader>
          <CardContent>
            <p className="whitespace-pre-wrap text-sm leading-prose">
              {data.surprise_text}
            </p>
          </CardContent>
        </Card>
      ) : null}

      {(() => {
        // Split goals into "carried over" vs "shaped this reflection".
        // Authoritative split is via weekly_reflections.new_goal_ids
        // (recorded by Card 3 when commit_goal lands). Goals created
        // earlier in the same week from the goals side menu are NOT
        // marked, so they correctly render under "Active".
        const newSet = new Set(data.new_goal_ids);
        const existing = data.active_goals.filter((g) => !newSet.has(g.id));
        const fresh = data.active_goals.filter((g) => newSet.has(g.id));
        return (
          <>
            {existing.length > 0 ? (
              <Card>
                <CardHeader className="pb-3">
                  <CardTitle className="font-serif text-base">Active goals</CardTitle>
                </CardHeader>
                <CardContent className="space-y-2">
                  {existing.map((g) => (
                    <GoalRow key={g.id} goal={g} note={data.goal_notes[g.id]} />
                  ))}
                </CardContent>
              </Card>
            ) : null}

            {fresh.length > 0 ? (
              <Card className="border-accent/40 bg-accent/5">
                <CardHeader className="pb-3">
                  <CardTitle className="inline-flex items-center gap-2 font-serif text-base">
                    <Sparkles className="h-4 w-4 text-accent" />
                    New goals
                  </CardTitle>
                  <p className="text-xs italic text-muted-foreground">
                    Shaped during this reflection.
                  </p>
                </CardHeader>
                <CardContent className="space-y-2">
                  {fresh.map((g) => (
                    <GoalRow key={g.id} goal={g} note={data.goal_notes[g.id]} fresh />
                  ))}
                </CardContent>
              </Card>
            ) : null}
          </>
        );
      })()}

      {allowReplay ? (
        <div className="flex justify-center pt-2">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => replay.mutate()}
            disabled={replay.isPending}
            className="gap-1.5 text-xs text-muted-foreground hover:text-foreground"
          >
            <RotateCcw className="h-3.5 w-3.5" />
            {replay.isPending ? "Replaying…" : "Replay weekly reflection"}
          </Button>
        </div>
      ) : null}
    </div>
  );
}

function GoalRow({
  goal: g,
  note,
  fresh,
}: {
  goal: Zone1GoalStatus;
  note?: string;
  fresh?: boolean;
}) {
  return (
    <div
      className={
        fresh
          ? "space-y-1 rounded-md border border-accent/40 bg-background/50 px-3 py-2"
          : "space-y-1 rounded-md border border-border/60 px-3 py-2"
      }
    >
      <div className="flex items-baseline justify-between gap-3">
        <p className="truncate text-sm font-medium">{g.title}</p>
        <p className="font-mono text-xs tabular-nums text-muted-foreground">
          {fresh ? "—" : `${g.kept_count}/${g.answered_count || 7}`}
        </p>
      </div>
      <p className="text-[11px] text-muted-foreground">
        {fresh
          ? `Starts ${g.start_date} · ends ${g.end_date}`
          : `Day ${g.day_index} of ${g.total_days} · ends ${g.end_date}`}
      </p>
      {note ? (
        <p className="border-l-2 border-accent/40 pl-2 text-xs italic text-muted-foreground">
          {note}
        </p>
      ) : null}
    </div>
  );
}
