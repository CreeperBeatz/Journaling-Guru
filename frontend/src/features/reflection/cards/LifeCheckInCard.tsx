import { useState } from "react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Slider } from "@/components/ui/slider";
import { Textarea } from "@/components/ui/textarea";
import { toast } from "@/components/ui/sonner";
import { cn } from "@/lib/utils";

import { LIFE_DOMAINS, type MonthlyReflectionBlock } from "../api";
import { useSetMonthlyRatings } from "../hooks";

const DEFAULT_SCORE = 5;
const NOTE_MAX = 600;

// LifeCheckInCard — the life check-in between the monthly letter and
// the reflection chat. One card per domain (PWI format: one "How
// satisfied…" item, 0–10 end-defined scale), stepped through with an
// optional "want to say why?" note on each. The global "life as a
// whole" item comes first (PWI/OECD ordering — domains must not prime
// the headline number); Belonging is the optional last step and can be
// skipped on its own.
//
// Ghost values show last month's score when prev_ratings exists. The
// whole check-in is skippable — ratings are nullable server-side and
// the chat degrades gracefully.
export function LifeCheckInCard({
  monthly,
  onDone,
}: {
  monthly: MonthlyReflectionBlock;
  onDone: () => void;
}) {
  const save = useSetMonthlyRatings();
  const reduce = useReducedMotion();
  const prev = monthly.prev_ratings ?? {};

  const [idx, setIdx] = useState(0);
  const [scores, setScores] = useState<Record<string, number>>(() => {
    const initial: Record<string, number> = {};
    for (const d of LIFE_DOMAINS) {
      const existing = monthly.ratings?.[d.key];
      if (existing !== undefined) initial[d.key] = existing;
    }
    return initial;
  });
  const [notes, setNotes] = useState<Record<string, string>>(
    () => ({ ...(monthly.rating_notes ?? {}) }),
  );

  const domain = LIFE_DOMAINS[idx];
  const isLast = idx === LIFE_DOMAINS.length - 1;
  const score = scores[domain.key] ?? prev[domain.key] ?? DEFAULT_SCORE;
  const note = notes[domain.key] ?? "";

  const commitCurrent = () => {
    // Touching Next commits the visible slider value even if the user
    // never dragged it — the shown number is what they accepted.
    setScores((s) => ({ ...s, [domain.key]: score }));
  };

  const submit = async (finalScores: Record<string, number>) => {
    const trimmedNotes: Record<string, string> = {};
    for (const [k, v] of Object.entries(notes)) {
      const t = v.trim();
      if (t !== "" && finalScores[k] !== undefined) trimmedNotes[k] = t;
    }
    try {
      await save.mutateAsync({ ratings: finalScores, notes: trimmedNotes });
      onDone();
    } catch (err) {
      toast.error("Couldn't save your check-in", {
        description: err instanceof Error ? err.message : "try again",
      });
    }
  };

  const onNext = () => {
    const finalScores = { ...scores, [domain.key]: score };
    setScores(finalScores);
    if (isLast) {
      void submit(finalScores);
    } else {
      setIdx(idx + 1);
    }
  };

  // The optional Belonging step can be skipped on its own: finish
  // without rating it.
  const onSkipOptional = () => {
    void submit({ ...scores });
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between px-1">
        <p className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground">
          Thinking about this past month…
        </p>
        <p className="font-mono text-[11px] tabular-nums text-muted-foreground">
          {idx + 1} / {LIFE_DOMAINS.length}
        </p>
      </div>

      <div className="flex gap-1 px-1" aria-hidden>
        {LIFE_DOMAINS.map((d, i) => (
          <span
            key={d.key}
            className={cn(
              "h-1 flex-1 rounded-full transition-colors",
              i < idx
                ? "bg-accent/70"
                : i === idx
                  ? "bg-primary"
                  : "bg-border",
            )}
          />
        ))}
      </div>

      <AnimatePresence mode="wait" initial={false}>
        <motion.div
          key={domain.key}
          initial={reduce ? false : { opacity: 0, x: 24 }}
          animate={{ opacity: 1, x: 0 }}
          exit={reduce ? undefined : { opacity: 0, x: -24 }}
          transition={{ duration: 0.18, ease: "easeOut" }}
        >
          <Card className={cn(idx === 0 && "border-accent/40 bg-accent/5")}>
            <CardContent className="space-y-5 px-6 py-6">
              <div className="space-y-1">
                <h2 className="font-serif text-xl">{domain.label}</h2>
                <p className="text-sm text-muted-foreground">
                  How satisfied are you with… {domain.question.charAt(0).toLowerCase() + domain.question.slice(1)}
                </p>
              </div>

              <div className="space-y-1.5">
                <div className="flex items-baseline justify-end">
                  <p className="font-mono text-2xl tabular-nums">
                    {score}
                    {prev[domain.key] !== undefined ? (
                      <span className="ml-2 text-xs text-muted-foreground">
                        was {prev[domain.key]} last month
                      </span>
                    ) : null}
                  </p>
                </div>
                <Slider
                  min={0}
                  max={10}
                  step={1}
                  value={[score]}
                  onValueChange={([v]) =>
                    setScores((s) => ({ ...s, [domain.key]: v }))
                  }
                  aria-label={domain.label}
                />
                <div className="flex justify-between font-mono text-[10px] uppercase tracking-wide text-muted-foreground/70">
                  <span>Not at all satisfied</span>
                  <span>Completely satisfied</span>
                </div>
              </div>

              <div className="space-y-1">
                <label
                  htmlFor={`note-${domain.key}`}
                  className="text-xs font-medium text-foreground/85"
                >
                  Want to say why? <span className="font-normal text-muted-foreground">(optional)</span>
                </label>
                <Textarea
                  id={`note-${domain.key}`}
                  value={note}
                  onChange={(e) =>
                    setNotes((n) => ({ ...n, [domain.key]: e.target.value }))
                  }
                  placeholder="A sentence or two, if something comes to mind…"
                  maxLength={NOTE_MAX}
                  rows={2}
                  className="min-h-[3.5rem] text-sm"
                />
              </div>

              <div className="flex items-center justify-between pt-1">
                <div className="flex items-center gap-1">
                  {idx > 0 ? (
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        commitCurrent();
                        setIdx(idx - 1);
                      }}
                      disabled={save.isPending}
                      className="text-muted-foreground"
                    >
                      Back
                    </Button>
                  ) : (
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={onDone}
                      disabled={save.isPending}
                      className="text-muted-foreground"
                    >
                      Not today
                    </Button>
                  )}
                </div>
                <div className="flex items-center gap-2">
                  {isLast && domain.optional ? (
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={onSkipOptional}
                      disabled={save.isPending}
                      className="text-muted-foreground"
                    >
                      Skip this one
                    </Button>
                  ) : null}
                  <Button type="button" onClick={onNext} disabled={save.isPending}>
                    {save.isPending
                      ? "Saving…"
                      : isLast
                        ? "Save & continue"
                        : "Next"}
                  </Button>
                </div>
              </div>
            </CardContent>
          </Card>
        </motion.div>
      </AnimatePresence>
    </div>
  );
}
