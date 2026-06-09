import { useEffect, useMemo, useState } from "react";
import { Loader2, X } from "lucide-react";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Textarea } from "@/components/ui/textarea";
import { StatusPill } from "@/components/ui/status-pill";
import { useDebouncedFlag } from "@/lib/useDebouncedFlag";
import { cn } from "@/lib/utils";

import type { DailyInput, DailyInputUpsertBody, TagDayLink } from "./api";
import { TagPicker } from "./TagPicker";

interface Props {
  // Existing data, or null when nothing has been logged yet.
  input: DailyInput | null;
  // The day's tag links (drainer + charger), as returned by GET
  // /api/daily/inputs. Split by role inside this component.
  tags: TagDayLink[];
  // Save callback. The hook layer turns "everything empty" into a delete.
  onSave: (body: DailyInputUpsertBody) => void;
  // Whether a save is currently in flight. Used for the spinner.
  isSaving: boolean;
  // Optional title override; defaults to "Today's check-in".
  title?: string;
}

// DailyInputs — the Manual tab's five-prompt Energy Audit. Five field
// cards in fixed order: mood faces / drainer + tags / charger + tags /
// gratitude / reflection. Save-on-blur for text; save-on-click for
// mood faces and tag picks. The hook layer collapses an all-empty save
// into a row delete.
export function DailyInputs({ input, tags, onSave, isSaving, title }: Props) {
  // Server snapshot — what we last saw from the server, used to detect
  // dirty state and to re-sync after a refetch lands.
  const initialDrainerIds = useMemo(
    () => tags.filter((t) => t.role === "drainer").map((t) => t.tag_id),
    [tags],
  );
  const initialChargerIds = useMemo(
    () => tags.filter((t) => t.role === "charger").map((t) => t.tag_id),
    [tags],
  );

  const [mood, setMood] = useState<number | null>(input?.mood ?? null);
  const [drainedText, setDrainedText] = useState(input?.drained_text ?? "");
  const [chargedText, setChargedText] = useState(input?.charged_text ?? "");
  const [gratitudeText, setGratitudeText] = useState(input?.gratitude_text ?? "");
  const [reflectionText, setReflectionText] = useState(input?.reflection_text ?? "");
  const [drainerIds, setDrainerIds] = useState<string[]>(initialDrainerIds);
  const [chargerIds, setChargerIds] = useState<string[]>(initialChargerIds);

  const [serverSnap, setServerSnap] = useState({
    mood: input?.mood ?? null,
    drainedText: input?.drained_text ?? "",
    chargedText: input?.charged_text ?? "",
    gratitudeText: input?.gratitude_text ?? "",
    reflectionText: input?.reflection_text ?? "",
    drainerIds: initialDrainerIds,
    chargerIds: initialChargerIds,
  });
  const showSpinner = useDebouncedFlag(isSaving, 300);

  // Re-sync when the server-state prop changes, but only when the user
  // hasn't started editing — drafts win.
  useEffect(() => {
    const next = {
      mood: input?.mood ?? null,
      drainedText: input?.drained_text ?? "",
      chargedText: input?.charged_text ?? "",
      gratitudeText: input?.gratitude_text ?? "",
      reflectionText: input?.reflection_text ?? "",
      drainerIds: initialDrainerIds,
      chargerIds: initialChargerIds,
    };
    const stillPristine =
      mood === serverSnap.mood &&
      drainedText === serverSnap.drainedText &&
      chargedText === serverSnap.chargedText &&
      gratitudeText === serverSnap.gratitudeText &&
      reflectionText === serverSnap.reflectionText &&
      idsEqual(drainerIds, serverSnap.drainerIds) &&
      idsEqual(chargerIds, serverSnap.chargerIds);
    if (stillPristine) {
      setMood(next.mood);
      setDrainedText(next.drainedText);
      setChargedText(next.chargedText);
      setGratitudeText(next.gratitudeText);
      setReflectionText(next.reflectionText);
      setDrainerIds(next.drainerIds);
      setChargerIds(next.chargerIds);
    }
    setServerSnap(next);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [input?.id, input?.updated_at, tags]);

  const dirty =
    mood !== serverSnap.mood ||
    drainedText !== serverSnap.drainedText ||
    chargedText !== serverSnap.chargedText ||
    gratitudeText !== serverSnap.gratitudeText ||
    reflectionText !== serverSnap.reflectionText ||
    !idsEqual(drainerIds, serverSnap.drainerIds) ||
    !idsEqual(chargerIds, serverSnap.chargerIds);

  const flush = () => {
    if (!dirty) return;
    onSave({
      mood,
      drained_text: drainedText,
      charged_text: chargedText,
      gratitude_text: gratitudeText,
      reflection_text: reflectionText,
      drained_tag_ids: drainerIds,
      charged_tag_ids: chargerIds,
    });
  };

  // Tag picks should commit on the next microtask so the optimistic
  // pill renders, then save fires.
  const setDrainerIdsAndCommit = (ids: string[]) => {
    setDrainerIds(ids);
    queueMicrotask(() =>
      onSave({
        mood,
        drained_text: drainedText,
        charged_text: chargedText,
        gratitude_text: gratitudeText,
        reflection_text: reflectionText,
        drained_tag_ids: ids,
        charged_tag_ids: chargerIds,
      }),
    );
  };
  const setChargerIdsAndCommit = (ids: string[]) => {
    setChargerIds(ids);
    queueMicrotask(() =>
      onSave({
        mood,
        drained_text: drainedText,
        charged_text: chargedText,
        gratitude_text: gratitudeText,
        reflection_text: reflectionText,
        drained_tag_ids: drainerIds,
        charged_tag_ids: ids,
      }),
    );
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
        <MoodFaces
          value={mood}
          onChange={(v) => {
            setMood(v);
            queueMicrotask(() =>
              onSave({
                mood: v,
                drained_text: drainedText,
                charged_text: chargedText,
                gratitude_text: gratitudeText,
                reflection_text: reflectionText,
                drained_tag_ids: drainerIds,
                charged_tag_ids: chargerIds,
              }),
            );
          }}
        />
        <FieldSection
          label="What drained you?"
          help="In your words. ‘Nothing today’ is a valid answer."
        >
          <Textarea
            value={drainedText}
            onChange={(e) => setDrainedText(e.target.value)}
            onBlur={flush}
            rows={2}
            maxLength={1000}
            placeholder="Back-to-back meetings…"
            className={fieldClass}
          />
          <TagPicker
            valence="negative"
            selectedIds={drainerIds}
            onChange={setDrainerIdsAndCommit}
            placeholder="drainer…"
          />
        </FieldSection>
        <FieldSection
          label="What charged you?"
          help="In your words. ‘Nothing today’ is fine."
        >
          <Textarea
            value={chargedText}
            onChange={(e) => setChargedText(e.target.value)}
            onBlur={flush}
            rows={2}
            maxLength={1000}
            placeholder="A walk after lunch…"
            className={fieldClass}
          />
          <TagPicker
            valence="positive"
            selectedIds={chargerIds}
            onChange={setChargerIdsAndCommit}
            placeholder="charger…"
          />
        </FieldSection>
        <FieldSection label="What are you grateful for?">
          <Textarea
            value={gratitudeText}
            onChange={(e) => setGratitudeText(e.target.value)}
            onBlur={flush}
            rows={2}
            maxLength={1000}
            placeholder="One thing — small or large."
            className={fieldClass}
          />
        </FieldSection>
        <FieldSection label="Anything else on your mind?">
          <Textarea
            value={reflectionText}
            onChange={(e) => setReflectionText(e.target.value)}
            onBlur={flush}
            rows={3}
            maxLength={4000}
            placeholder="Optional — anything that doesn't fit the slots above."
            className={fieldClass}
          />
        </FieldSection>
      </CardContent>
    </Card>
  );
}

const fieldClass = cn(
  "border-transparent bg-transparent px-0 leading-prose text-body",
  "focus-visible:ring-0 focus-visible:ring-offset-0",
  "focus-visible:border-b-border focus-visible:border-b rounded-none",
);

function FieldSection({
  label,
  help,
  children,
}: {
  label: string;
  help?: string;
  children: React.ReactNode;
}) {
  return (
    <section className="space-y-2">
      <div className="flex items-baseline justify-between gap-3">
        <label className="text-xs uppercase tracking-wider text-muted-foreground">
          {label}
        </label>
        {help ? (
          <span className="text-[11px] italic text-muted-foreground">{help}</span>
        ) : null}
      </div>
      {children}
    </section>
  );
}

// ---------------- Mood faces ----------------

const FACES: Array<{ value: -2 | -1 | 0 | 1 | 2; emoji: string; label: string }> = [
  { value: -2, emoji: "😣", label: "Very bad" },
  { value: -1, emoji: "🙁", label: "Bad" },
  { value: 0, emoji: "😐", label: "Neutral" },
  { value: 1, emoji: "🙂", label: "Good" },
  { value: 2, emoji: "😄", label: "Very good" },
];

function MoodFaces({
  value,
  onChange,
}: {
  value: number | null;
  onChange: (v: number | null) => void;
}) {
  return (
    <section className="space-y-2">
      <div className="flex items-baseline justify-between gap-3">
        <label className="text-xs uppercase tracking-wider text-muted-foreground">
          Mood today
        </label>
        {value !== null ? (
          <button
            type="button"
            onClick={() => onChange(null)}
            aria-label="Clear mood"
            className="rounded p-1 text-muted-foreground hover:text-foreground"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        ) : null}
      </div>
      <div className="flex items-center gap-2">
        {FACES.map((f) => {
          const active = value === f.value;
          return (
            <button
              key={f.value}
              type="button"
              onClick={() => onChange(f.value)}
              aria-pressed={active}
              aria-label={f.label}
              className={cn(
                "h-12 w-12 rounded-full border text-2xl transition-transform",
                "flex items-center justify-center",
                active
                  ? "border-accent bg-accent/10 scale-110"
                  : "border-border bg-card hover:bg-secondary",
              )}
            >
              {f.emoji}
            </button>
          );
        })}
      </div>
    </section>
  );
}

// Order-insensitive: local state holds the user's click order while the
// server returns its own, so positional comparison would read the same
// tag set as dirty after a save round-trip.
function idsEqual(a: string[], b: string[]) {
  if (a.length !== b.length) return false;
  const sortedA = [...a].sort();
  const sortedB = [...b].sort();
  for (let i = 0; i < sortedA.length; i++) if (sortedA[i] !== sortedB[i]) return false;
  return true;
}
