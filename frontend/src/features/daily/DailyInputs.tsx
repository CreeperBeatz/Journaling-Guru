import { useEffect, useState } from "react";
import { Loader2, X } from "lucide-react";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Slider } from "@/components/ui/slider";
import { Textarea } from "@/components/ui/textarea";
import { StatusPill } from "@/components/ui/status-pill";
import { useDebouncedFlag } from "@/lib/useDebouncedFlag";
import { cn } from "@/lib/utils";

import type { DailyInput } from "./api";

interface Props {
  // Existing data, or null when nothing has been logged yet.
  input: DailyInput | null;
  // Save callback — `null` mood means "unset", empty strings are valid.
  // The hook layer turns "everything empty" into a delete.
  onSave: (body: { mood_score: number | null; emotions_text: string; notes: string }) => void;
  // Whether a save is currently in flight. Used for the spinner.
  isSaving: boolean;
  // Optional title override; defaults to "Today's check-in".
  title?: string;
}

export function DailyInputs({ input, onSave, isSaving, title }: Props) {
  // Mood: -1 means "no value" since Radix Slider can't show null. We
  // map between (slider value, internal nullable state) at the edges.
  const [mood, setMood] = useState<number | null>(input?.mood_score ?? null);
  const [emotionsText, setEmotionsText] = useState<string>(input?.emotions_text ?? "");
  const [notes, setNotes] = useState<string>(input?.notes ?? "");
  const [serverSnap, setServerSnap] = useState({
    mood: input?.mood_score ?? null,
    emotionsText: input?.emotions_text ?? "",
    notes: input?.notes ?? "",
  });
  const showSpinner = useDebouncedFlag(isSaving, 300);

  // Re-sync when the server-state prop changes (e.g. after refetch),
  // but only when the user hasn't started editing — drafts win.
  useEffect(() => {
    const serverMood = input?.mood_score ?? null;
    const serverEmotionsText = input?.emotions_text ?? "";
    const serverNotes = input?.notes ?? "";
    const stillPristine =
      eq(mood, serverSnap.mood) &&
      emotionsText === serverSnap.emotionsText &&
      notes === serverSnap.notes;
    if (stillPristine) {
      setMood(serverMood);
      setEmotionsText(serverEmotionsText);
      setNotes(serverNotes);
    }
    setServerSnap({ mood: serverMood, emotionsText: serverEmotionsText, notes: serverNotes });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [input?.id, input?.updated_at]);

  const dirty =
    !eq(mood, serverSnap.mood) ||
    emotionsText !== serverSnap.emotionsText ||
    notes !== serverSnap.notes;

  const flush = () => {
    if (!dirty) return;
    onSave({ mood_score: mood, emotions_text: emotionsText, notes });
  };

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between gap-3 pb-3">
        <CardTitle className="font-serif text-base">
          {title ?? "Today's check-in"}
        </CardTitle>
        <div className="flex items-center gap-2">
          {showSpinner ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
          ) : null}
          {dirty ? <StatusPill state="dirty" /> : null}
        </div>
      </CardHeader>
      <CardContent className="space-y-6">
        <MoodInput
          value={mood}
          onChange={setMood}
          onCommit={flush}
        />
        <EmotionsInput
          value={emotionsText}
          onChange={setEmotionsText}
          onCommit={flush}
        />
        <NotesInput
          value={notes}
          onChange={setNotes}
          onCommit={flush}
        />
      </CardContent>
    </Card>
  );
}

// ---------------- Mood ----------------

const MOOD_LABELS: Array<[number, string]> = [
  [4, "negative"],
  [6, "neutral"],
  [10, "positive"],
];

function moodLabel(score: number): string {
  for (const [hi, label] of MOOD_LABELS) if (score <= hi) return label;
  return "positive";
}

function moodLabelClass(score: number): string {
  if (score <= 4) return "text-destructive";
  if (score <= 6) return "text-muted-foreground";
  return "text-accent";
}

function MoodInput({
  value,
  onChange,
  onCommit,
}: {
  value: number | null;
  onChange: (v: number | null) => void;
  onCommit: () => void;
}) {
  const display = value ?? 5;
  const set = value !== null;
  return (
    <section className="space-y-2">
      <div className="flex items-baseline justify-between gap-3">
        <label className="text-xs uppercase tracking-wider text-muted-foreground">
          Mood
        </label>
        <div className="flex items-center gap-2">
          {set ? (
            <span className="font-mono text-base tabular-nums">
              {display}
              <span className="text-muted-foreground">/10</span>
              <span className={cn("ml-2 text-sm capitalize", moodLabelClass(display))}>
                {moodLabel(display)}
              </span>
            </span>
          ) : (
            <span className="text-sm text-muted-foreground italic">not set</span>
          )}
          {set ? (
            <button
              type="button"
              onClick={() => {
                onChange(null);
                // Commit immediately so unset propagates without a focus dance.
                queueMicrotask(onCommit);
              }}
              aria-label="Clear mood"
              title="Clear mood"
              className={cn(
                "ml-1 rounded p-1 text-muted-foreground transition-colors",
                "hover:bg-secondary hover:text-foreground",
              )}
            >
              <X className="h-3.5 w-3.5" />
            </button>
          ) : null}
        </div>
      </div>
      <Slider
        value={[display]}
        min={1}
        max={10}
        step={1}
        ticks
        onValueChange={([v]) => onChange(v)}
        onValueCommit={onCommit}
      />
      {!set ? (
        <button
          type="button"
          onClick={() => {
            onChange(5);
            queueMicrotask(onCommit);
          }}
          className="text-xs text-accent underline-offset-4 hover:underline"
        >
          Tap to set
        </button>
      ) : null}
    </section>
  );
}

// ---------------- Emotions ----------------

const EMOTIONS_TEXT_MAX = 1000;

// Free-text emotions. The server fires an async River job that
// classifies this into Plutchik's wheel (base + subtype); the
// classified emotions only appear in summaries — never echoed back
// here. Save-on-blur, same as Notes.
function EmotionsInput({
  value,
  onChange,
  onCommit,
}: {
  value: string;
  onChange: (v: string) => void;
  onCommit: () => void;
}) {
  return (
    <section className="space-y-2">
      <label className="text-xs uppercase tracking-wider text-muted-foreground">
        Emotions felt
      </label>
      <Textarea
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onBlur={onCommit}
        rows={3}
        maxLength={EMOTIONS_TEXT_MAX}
        placeholder="Anxious before the meeting, then relieved when it went well…"
        className={cn(
          "border-transparent bg-transparent px-0 leading-prose text-body",
          "focus-visible:ring-0 focus-visible:ring-offset-0",
          "focus-visible:border-b-border focus-visible:border-b rounded-none",
        )}
      />
    </section>
  );
}

// ---------------- Notes ----------------

function NotesInput({
  value,
  onChange,
  onCommit,
}: {
  value: string;
  onChange: (v: string) => void;
  onCommit: () => void;
}) {
  return (
    <section className="space-y-2">
      <label className="text-xs uppercase tracking-wider text-muted-foreground">
        Notes
      </label>
      <Textarea
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onBlur={onCommit}
        rows={3}
        maxLength={4000}
        placeholder="Anything else worth remembering — feeds into the daily reflection."
        className={cn(
          "border-transparent bg-transparent px-0 leading-prose text-body",
          "focus-visible:ring-0 focus-visible:ring-offset-0",
          "focus-visible:border-b-border focus-visible:border-b rounded-none",
        )}
      />
    </section>
  );
}

// ---------------- helpers ----------------

function eq(a: number | null, b: number | null) {
  return a === b;
}
