import { useEffect, useState } from "react";
import { Loader2, X } from "lucide-react";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Slider } from "@/components/ui/slider";
import { Textarea } from "@/components/ui/textarea";
import { StatusPill } from "@/components/ui/status-pill";
import { useDebouncedFlag } from "@/lib/useDebouncedFlag";
import { cn } from "@/lib/utils";

import type { DailyInput } from "./api";

// Curated palette of common emotions. Tap to toggle. Custom tags are
// stored alongside (lowercase, deduped, trimmed). Update this list freely
// — it's purely frontend; the backend stores whatever string the API
// gets after normalization.
const EMOTION_SUGGESTIONS = [
  "happy",
  "grateful",
  "calm",
  "content",
  "excited",
  "proud",
  "hopeful",
  "loved",
  "energized",
  "focused",
  "curious",
  "surprised",
  "contemplative",
  "tired",
  "busy",
  "anxious",
  "stressed",
  "frustrated",
  "sad",
  "lonely",
  "overwhelmed",
  "angry",
  "restless",
  "disappointed",
] as const;

const MAX_EMOTIONS = 8;

interface Props {
  // Existing data, or null when nothing has been logged yet.
  input: DailyInput | null;
  // Save callback — `null` mood means "unset", empty arrays / strings
  // are valid. The hook layer turns "everything empty" into a delete.
  onSave: (body: { mood_score: number | null; emotions: string[]; notes: string }) => void;
  // Whether a save is currently in flight. Used for the spinner.
  isSaving: boolean;
  // Optional title override; defaults to "Today's check-in".
  title?: string;
}

export function DailyInputs({ input, onSave, isSaving, title }: Props) {
  // Mood: -1 means "no value" since Radix Slider can't show null. We
  // map between (slider value, internal nullable state) at the edges.
  const [mood, setMood] = useState<number | null>(input?.mood_score ?? null);
  const [emotions, setEmotions] = useState<string[]>(input?.emotions ?? []);
  const [notes, setNotes] = useState<string>(input?.notes ?? "");
  const [serverSnap, setServerSnap] = useState({
    mood: input?.mood_score ?? null,
    emotions: input?.emotions ?? [],
    notes: input?.notes ?? "",
  });
  const showSpinner = useDebouncedFlag(isSaving, 300);

  // Re-sync when the server-state prop changes (e.g. after refetch),
  // but only when the user hasn't started editing — drafts win.
  useEffect(() => {
    const serverMood = input?.mood_score ?? null;
    const serverEmotions = input?.emotions ?? [];
    const serverNotes = input?.notes ?? "";
    const stillPristine =
      eq(mood, serverSnap.mood) &&
      sameSet(emotions, serverSnap.emotions) &&
      notes === serverSnap.notes;
    if (stillPristine) {
      setMood(serverMood);
      setEmotions(serverEmotions);
      setNotes(serverNotes);
    }
    setServerSnap({ mood: serverMood, emotions: serverEmotions, notes: serverNotes });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [input?.id, input?.updated_at]);

  const dirty =
    !eq(mood, serverSnap.mood) ||
    !sameSet(emotions, serverSnap.emotions) ||
    notes !== serverSnap.notes;

  const flush = () => {
    if (!dirty) return;
    onSave({ mood_score: mood, emotions, notes });
  };

  const toggleEmotion = (e: string) => {
    const norm = e.trim().toLowerCase();
    if (!norm) return;
    setEmotions((prev) => {
      if (prev.includes(norm)) return prev.filter((x) => x !== norm);
      if (prev.length >= MAX_EMOTIONS) return prev;
      return [...prev, norm];
    });
  };

  // Save emotions toggles immediately — they're a single-click action,
  // a blur-to-save delay would feel laggy. Mood + notes save on blur.
  useEffect(() => {
    if (sameSet(emotions, serverSnap.emotions)) return;
    onSave({ mood_score: mood, emotions, notes });
    // We intentionally exclude mood/notes — toggling an emotion shouldn't
    // also commit a half-typed note.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [emotions]);

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
        <EmotionInput
          selected={emotions}
          onToggle={toggleEmotion}
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

function EmotionInput({
  selected,
  onToggle,
}: {
  selected: string[];
  onToggle: (e: string) => void;
}) {
  const [draft, setDraft] = useState("");
  const customEmotions = selected.filter(
    (s) => !EMOTION_SUGGESTIONS.includes(s as (typeof EMOTION_SUGGESTIONS)[number]),
  );

  const submit = () => {
    const norm = draft.trim().toLowerCase();
    if (!norm) return;
    if (norm.length > 32) return;
    onToggle(norm);
    setDraft("");
  };

  return (
    <section className="space-y-2">
      <div className="flex items-baseline justify-between gap-3">
        <label className="text-xs uppercase tracking-wider text-muted-foreground">
          Emotions felt
        </label>
        <span className="font-mono text-[11px] tabular-nums text-muted-foreground">
          {selected.length} / {MAX_EMOTIONS}
        </span>
      </div>
      <div className="flex flex-wrap gap-1.5">
        {EMOTION_SUGGESTIONS.map((e) => {
          const on = selected.includes(e);
          return (
            <button
              key={e}
              type="button"
              onClick={() => onToggle(e)}
              className={cn(
                "rounded-full border px-2.5 py-1 text-xs capitalize transition-colors",
                on
                  ? "border-accent bg-accent/15 text-foreground"
                  : "border-border bg-card text-muted-foreground hover:border-accent/40 hover:text-foreground",
              )}
              aria-pressed={on}
            >
              {e}
            </button>
          );
        })}
        {customEmotions.map((e) => (
          <button
            key={e}
            type="button"
            onClick={() => onToggle(e)}
            className="group flex items-center gap-1 rounded-full border border-accent bg-accent/15 px-2.5 py-1 text-xs capitalize"
            aria-label={`Remove ${e}`}
          >
            {e}
            <X className="h-3 w-3 opacity-60 transition-opacity group-hover:opacity-100" />
          </button>
        ))}
      </div>
      <div className="flex items-center gap-2 pt-1">
        <input
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              submit();
            }
          }}
          placeholder="Custom emotion…"
          maxLength={32}
          className={cn(
            "h-8 flex-1 rounded-md border border-border bg-transparent px-2 text-sm",
            "placeholder:text-muted-foreground",
            "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
          )}
        />
        <button
          type="button"
          onClick={submit}
          disabled={!draft.trim() || selected.length >= MAX_EMOTIONS}
          className={cn(
            "h-8 rounded-md border border-border bg-transparent px-3 text-xs transition-colors",
            "hover:bg-secondary",
            "disabled:opacity-40 disabled:hover:bg-transparent",
          )}
        >
          Add
        </button>
      </div>
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

function sameSet(a: string[], b: string[]) {
  if (a.length !== b.length) return false;
  const setB = new Set(b);
  for (const x of a) if (!setB.has(x)) return false;
  return true;
}
