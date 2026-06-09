import { useState } from "react";
import { ChevronDown } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Slider } from "@/components/ui/slider";
import { toast } from "@/components/ui/sonner";
import { cn } from "@/lib/utils";

import { LIFE_DOMAINS, type MonthlyReflectionBlock } from "../api";
import { useSetMonthlyRatings } from "../hooks";

const DEFAULT_SCORE = 5;

// LifeCheckInCard — the 30-second life check-in between the monthly
// letter and the reflection chat. PWI format: one "How satisfied…"
// slider per domain, 0–10 end-defined ("Not at all satisfied" →
// "Completely satisfied"), global item first so the domains don't prime
// the headline number. Belonging is opt-in, collapsed by default (and
// auto-expanded when a previous month rated it).
//
// Ghost dots show last month's value when prev_ratings exists. Skippable
// — ratings are nullable server-side and the chat degrades gracefully.
export function LifeCheckInCard({
  monthly,
  onDone,
}: {
  monthly: MonthlyReflectionBlock;
  onDone: () => void;
}) {
  const save = useSetMonthlyRatings();
  const prev = monthly.prev_ratings ?? {};
  const [scores, setScores] = useState<Record<string, number>>(() => {
    const initial: Record<string, number> = {};
    for (const d of LIFE_DOMAINS) {
      const existing = monthly.ratings?.[d.key];
      if (existing !== undefined) {
        initial[d.key] = existing;
      } else if (!d.optional) {
        initial[d.key] = prev[d.key] ?? DEFAULT_SCORE;
      }
    }
    return initial;
  });
  const [showOptional, setShowOptional] = useState(
    () => LIFE_DOMAINS.some((d) => d.optional && (monthly.ratings?.[d.key] !== undefined || prev[d.key] !== undefined)),
  );

  const setScore = (key: string, value: number) => {
    setScores((s) => ({ ...s, [key]: value }));
  };

  const onSubmit = async () => {
    try {
      await save.mutateAsync(scores);
      onDone();
    } catch (err) {
      toast.error("Couldn't save your check-in", {
        description: err instanceof Error ? err.message : "try again",
      });
    }
  };

  const visibleDomains = LIFE_DOMAINS.filter((d) => !d.optional || showOptional);

  return (
    <Card>
      <CardHeader className="pb-4">
        <CardTitle className="font-serif text-base">One quick check-in</CardTitle>
        <p className="text-xs italic text-muted-foreground">
          Thinking about this past month — how satisfied are you with…
        </p>
      </CardHeader>
      <CardContent className="space-y-5">
        {visibleDomains.map((d, idx) => (
          <div key={d.key} className={cn("space-y-1.5", idx === 0 && "rounded-lg border border-accent/30 bg-accent/5 px-3 py-3")}>
            <div className="flex items-baseline justify-between gap-3">
              <p className="text-sm font-medium">{d.label}</p>
              <p className="font-mono text-sm tabular-nums text-muted-foreground">
                {scores[d.key] ?? "—"}
                {prev[d.key] !== undefined && scores[d.key] !== undefined ? (
                  <span className="ml-1.5 text-[10px] opacity-70">
                    (was {prev[d.key]})
                  </span>
                ) : null}
              </p>
            </div>
            <p className="text-xs text-muted-foreground">{d.question}</p>
            <Slider
              min={0}
              max={10}
              step={1}
              value={[scores[d.key] ?? DEFAULT_SCORE]}
              onValueChange={([v]) => setScore(d.key, v)}
              aria-label={d.label}
            />
            <div className="flex justify-between font-mono text-[10px] uppercase tracking-wide text-muted-foreground/70">
              <span>Not at all satisfied</span>
              <span>Completely satisfied</span>
            </div>
          </div>
        ))}

        {!showOptional ? (
          <button
            type="button"
            onClick={() => {
              setShowOptional(true);
              const belonging = LIFE_DOMAINS.find((d) => d.optional);
              if (belonging && scores[belonging.key] === undefined) {
                setScore(belonging.key, prev[belonging.key] ?? DEFAULT_SCORE);
              }
            }}
            className="inline-flex items-center gap-1 text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
          >
            <ChevronDown className="h-3 w-3" />
            Add Belonging (optional)
          </button>
        ) : null}

        <div className="flex items-center justify-end gap-2 pt-1">
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
          <Button type="button" onClick={onSubmit} disabled={save.isPending}>
            {save.isPending ? "Saving…" : "Save & continue"}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
