import { useEffect, useState } from "react";
import { CheckCircle2, RotateCcw, Sparkles } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Textarea } from "@/components/ui/textarea";

import type { Zone1GoalStatus } from "@/features/summary/api";

import { PatchReflectionBody, ReflectionResponse } from "./api";
import { WeeklySynthesisCard } from "./cards/WeeklySynthesisCard";
import { usePatchReflection, useReplayReflection } from "./hooks";

interface Props {
  data: ReflectionResponse;
  /** Override the patch behavior — used by the History view to target
   * a past week's row. Defaults to this-week's PATCH. */
  onPatch?: (body: PatchReflectionBody) => void;
  /** Whether onPatch is currently in flight. */
  patchPending?: boolean;
  /** Hide the Replay button in History (replay only makes sense for
   * the current week). Defaults to true. */
  showReplay?: boolean;
  /** Hide the header — used when WeeklySummary is embedded under a
   * page that already has its own header (e.g. History). */
  showHeader?: boolean;
}

// WeeklySummary — the "Summary" tab content on /weekly. Renders the
// letter, plus *editable* fields for the user's own touches: the
// surprise/continuity sentence and per-goal notes (manual-mode-style,
// save on blur via PATCH /api/reflection/this-week).
//
// Reused by the History view (HistoryWeeklyReflection) with `onPatch`
// pointed at the historical patch endpoint and showReplay=false.
export function WeeklySummary({
  data,
  onPatch,
  patchPending,
  showReplay = true,
  showHeader = true,
}: Props) {
  const completedWhen = data.completed_at ? new Date(data.completed_at) : null;
  const newSet = new Set(data.new_goal_ids);
  const existingGoals = data.active_goals.filter((g) => !newSet.has(g.id));
  const freshGoals = data.active_goals.filter((g) => newSet.has(g.id));

  const thisWeekPatch = usePatchReflection();
  const replay = useReplayReflection();
  const patchFn = onPatch ?? ((body: PatchReflectionBody) => thisWeekPatch.mutate(body));
  const patching = onPatch ? !!patchPending : thisWeekPatch.isPending;

  return (
    <div className="space-y-6">
      {showHeader ? (
        <header className="space-y-1">
          <p className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground">
            Weekly reflection
          </p>
          <h1 className="font-serif text-h1 inline-flex items-center gap-2">
            {completedWhen ? (
              <CheckCircle2 className="h-6 w-6 text-accent" />
            ) : null}
            This week
          </h1>
          <p className="text-sm text-muted-foreground">
            {data.week_start} → {data.week_end}
            {completedWhen ? ` · wrapped ${completedWhen.toLocaleString()}` : ""}
          </p>
        </header>
      ) : null}

      <WeeklySynthesisCard data={data} />

      <EditableSurpriseCard
        initialText={data.surprise_text}
        onSave={(text) => patchFn({ surprise_text: text })}
        saving={patching}
      />

      {existingGoals.length > 0 ? (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="font-serif text-base">Active goals</CardTitle>
            <p className="text-xs italic text-muted-foreground">
              Add a note for any goal you want to remember a thought about.
            </p>
          </CardHeader>
          <CardContent className="space-y-2">
            {existingGoals.map((g) => (
              <EditableGoalRow
                key={g.id}
                goal={g}
                note={data.goal_notes[g.id] ?? ""}
                onSaveNote={(text) =>
                  patchFn({ goal_id: g.id, goal_note: text })
                }
              />
            ))}
          </CardContent>
        </Card>
      ) : null}

      {freshGoals.length > 0 ? (
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
            {freshGoals.map((g) => (
              <EditableGoalRow
                key={g.id}
                goal={g}
                note={data.goal_notes[g.id] ?? ""}
                onSaveNote={(text) =>
                  patchFn({ goal_id: g.id, goal_note: text })
                }
                fresh
              />
            ))}
          </CardContent>
        </Card>
      ) : null}

      {showReplay ? (
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

// EditableSurpriseCard — the "thing to remember" field, save-on-blur.
function EditableSurpriseCard({
  initialText,
  onSave,
  saving,
}: {
  initialText: string;
  onSave: (text: string) => void;
  saving: boolean;
}) {
  const [text, setText] = useState(initialText);
  const [dirty, setDirty] = useState(false);

  // Reset local state when the server value changes from underneath us
  // (e.g. extraction sentence arrives after wrap-up). Only when we're
  // not actively editing.
  useEffect(() => {
    if (!dirty) setText(initialText);
  }, [initialText, dirty]);

  const handleBlur = () => {
    if (!dirty) return;
    if (text === initialText) {
      setDirty(false);
      return;
    }
    onSave(text);
    setDirty(false);
  };

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="font-serif text-base">
          The thing to remember
        </CardTitle>
        <p className="text-xs italic text-muted-foreground">
          One line for next week's letter. Edit freely — the chat distils
          it for you on wrap-up, but you can rewrite it any time.
        </p>
      </CardHeader>
      <CardContent>
        <Textarea
          value={text}
          onChange={(e) => {
            setText(e.target.value);
            setDirty(true);
          }}
          onBlur={handleBlur}
          placeholder="One line you want next week to remember…"
          maxLength={500}
          className="min-h-[3rem]"
          disabled={saving}
        />
      </CardContent>
    </Card>
  );
}

// EditableGoalRow — same shape as the read-only GoalRow but with a
// save-on-blur note textarea.
function EditableGoalRow({
  goal: g,
  note,
  onSaveNote,
  fresh,
}: {
  goal: Zone1GoalStatus;
  note: string;
  onSaveNote: (text: string) => void;
  fresh?: boolean;
}) {
  const [text, setText] = useState(note);
  const [dirty, setDirty] = useState(false);
  useEffect(() => {
    if (!dirty) setText(note);
  }, [note, dirty]);

  const handleBlur = () => {
    if (!dirty) return;
    if (text.trim() === note.trim()) {
      setDirty(false);
      return;
    }
    onSaveNote(text.trim());
    setDirty(false);
  };

  return (
    <div
      className={
        fresh
          ? "space-y-2 rounded-md border border-accent/40 bg-background/50 px-3 py-2"
          : "space-y-2 rounded-md border border-border/60 px-3 py-2"
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
      <Textarea
        value={text}
        onChange={(e) => {
          setText(e.target.value);
          setDirty(true);
        }}
        onBlur={handleBlur}
        placeholder="A note for this goal…"
        maxLength={1000}
        className="min-h-[2.25rem] text-xs"
      />
    </div>
  );
}
