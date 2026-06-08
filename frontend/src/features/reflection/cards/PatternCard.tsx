import { ArrowDownRight, ArrowRight, ArrowUpRight } from "lucide-react";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";

import { ReflectionResponse, ReflectionTagRow } from "../api";

const LOW_CONFIDENCE_THRESHOLD = 7;

// PatternCard — read-only "what the week looked like" view. Shared by
// the wizard Card 1 + the Done page + the History weekly tab.
export function PatternCard({ data }: { data: ReflectionResponse }) {
  const moodDelta = computeMoodDelta(data.mood_avg, data.mood_avg_prior);
  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="font-serif text-base">What the week looked like</CardTitle>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="flex flex-wrap items-baseline gap-x-6 gap-y-2">
          <div>
            <p className="text-[11px] uppercase tracking-wider text-muted-foreground">
              Avg mood
            </p>
            <p className="font-mono text-2xl tabular-nums">
              {fmtMood(data.mood_avg)}
            </p>
          </div>
          {moodDelta ? (
            <p
              className={cn(
                "inline-flex items-center gap-0.5 text-sm font-medium",
                moodDelta.direction === "up"
                  ? "text-accent"
                  : moodDelta.direction === "down"
                    ? "text-destructive/80"
                    : "text-muted-foreground",
              )}
            >
              {moodDelta.direction === "up" ? (
                <ArrowUpRight className="h-4 w-4" />
              ) : moodDelta.direction === "down" ? (
                <ArrowDownRight className="h-4 w-4" />
              ) : (
                <ArrowRight className="h-4 w-4" />
              )}
              {moodDelta.label}
            </p>
          ) : null}
        </div>

        <div className="grid gap-6 md:grid-cols-2">
          <DeltaTagTable
            title="Top drainers"
            tone="negative"
            rows={data.drainers}
            empty="Nothing surfaced as a drainer this week."
          />
          <DeltaTagTable
            title="Top chargers"
            tone="positive"
            rows={data.chargers}
            empty="Nothing surfaced as a charger this week."
          />
        </div>

        {data.gratitude_items.length > 0 ? (
          <div className="space-y-2">
            <h3 className="text-[11px] uppercase tracking-wider text-muted-foreground">
              Gratitude this week
            </h3>
            <ul className="space-y-1 text-sm">
              {data.gratitude_items.map((g) => (
                <li
                  key={g.local_date}
                  className="border-b border-border/40 pb-1 last:border-0"
                >
                  <span className="font-mono text-[11px] tabular-nums text-muted-foreground">
                    {g.local_date}
                  </span>{" "}
                  · {g.text}
                </li>
              ))}
            </ul>
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}

function DeltaTagTable({
  title,
  tone,
  rows,
  empty,
}: {
  title: string;
  tone: "positive" | "negative";
  rows: ReflectionTagRow[];
  empty: string;
}) {
  return (
    <div className="space-y-2">
      <h3
        className={cn(
          "text-xs font-medium uppercase tracking-wider",
          tone === "positive" ? "text-accent" : "text-destructive/80",
        )}
      >
        {title}
      </h3>
      {rows.length === 0 ? (
        <p className="text-xs italic text-muted-foreground">{empty}</p>
      ) : (
        <ul className="space-y-1">
          {rows.map((row) => {
            const deltaIcon =
              row.delta_vs_prior > 0 ? (
                <ArrowUpRight className="h-3 w-3" />
              ) : row.delta_vs_prior < 0 ? (
                <ArrowDownRight className="h-3 w-3" />
              ) : null;
            return (
              <li
                key={row.tag_id}
                className="flex items-baseline justify-between gap-3 border-b border-border/40 pb-1 text-sm last:border-0"
              >
                <span className="min-w-0 flex-1 truncate">
                  {row.label}
                  {row.appearances < LOW_CONFIDENCE_THRESHOLD ? (
                    <span className="ml-2 text-[10px] uppercase tracking-wider text-muted-foreground/70">
                      low confidence
                    </span>
                  ) : null}
                </span>
                <span className="inline-flex shrink-0 items-center gap-1.5 font-mono text-xs tabular-nums text-muted-foreground">
                  {row.appearances}d
                  {deltaIcon ? (
                    <span
                      className={cn(
                        "inline-flex items-center",
                        row.delta_vs_prior > 0
                          ? tone === "positive"
                            ? "text-accent"
                            : "text-destructive/80"
                          : tone === "positive"
                            ? "text-destructive/80"
                            : "text-accent",
                      )}
                    >
                      {deltaIcon}
                      {Math.abs(row.delta_vs_prior)}
                    </span>
                  ) : null}
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

function fmtMood(score: number | null): string {
  if (score === null) return "—";
  // Signed -2..+2 scale: make the sign explicit so 0=neutral reads naturally.
  const s = score.toFixed(1);
  return score > 0 ? `+${s}` : s;
}

function computeMoodDelta(
  current: number | null,
  prior: number | null,
): { direction: "up" | "down" | "flat"; label: string } | null {
  if (current === null || prior === null) return null;
  const diff = current - prior;
  if (Math.abs(diff) < 0.05) return { direction: "flat", label: "flat vs last week" };
  const sign = diff > 0 ? "+" : "";
  return {
    direction: diff > 0 ? "up" : "down",
    label: `${sign}${diff.toFixed(1)} vs last week`,
  };
}
