import { useNavigate, useParams } from "react-router-dom";
import { ArrowLeft, Loader2, RefreshCw } from "lucide-react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

import { useRegenerateSummary, useSummary } from "./hooks";

const periodLabel: Record<string, string> = {
  day: "Daily",
  week: "Weekly",
  month: "Monthly",
  year: "Yearly",
};

export function SummaryDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const { data, isPending, isError } = useSummary(id);
  const regen = useRegenerateSummary();

  if (isPending) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-9 w-72" />
        <Skeleton className="h-4 w-48" />
        <Skeleton className="h-64 w-full rounded-xl" />
      </div>
    );
  }
  if (isError || !data) {
    return (
      <div className="space-y-4">
        <Button variant="ghost" size="sm" onClick={() => navigate(-1)}>
          <ArrowLeft className="h-4 w-4" /> Back
        </Button>
        <p className="text-sm text-destructive">Couldn't load this summary.</p>
      </div>
    );
  }

  const meta = data.metadata ?? {};
  const generated = new Date(data.generated_at);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-3">
        <Button variant="ghost" size="sm" onClick={() => navigate(-1)}>
          <ArrowLeft className="h-4 w-4" /> Back
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={regen.isPending}
          onClick={() =>
            regen.mutate({
              period_type: data.period_type,
              period_start: data.period_start,
            })
          }
        >
          {regen.isPending ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <RefreshCw className="h-4 w-4" />
          )}
          Regenerate
        </Button>
      </div>

      <header className="space-y-1">
        <p className="text-xs uppercase tracking-wider text-muted-foreground">
          {periodLabel[data.period_type] ?? data.period_type} reflection
        </p>
        <h1 className="font-serif text-h1">{periodHeading(data.period_type, data.period_start, data.period_end)}</h1>
        <p className="text-xs font-mono text-muted-foreground tabular-nums">
          generated {generated.toLocaleString()} · {data.model}
        </p>
      </header>

      {(meta.mood_label || meta.mood_score != null || (meta.emotions?.length ?? 0) > 0 || (meta.topics?.length ?? 0) > 0) ? (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="font-serif text-base">At a glance</CardTitle>
            <CardDescription>
              {data.period_type === "day"
                ? "Extracted from this day's reflection."
                : "Aggregated from the constituent daily summaries."}
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-3 sm:grid-cols-2">
            {meta.mood_score != null ? (
              <Stat label="Mood">
                <span className="font-mono text-xl tabular-nums">{meta.mood_score.toFixed(1)}</span>
                <span className="text-muted-foreground"> /10</span>
                {meta.mood_label ? (
                  <span className={cn("ml-2 text-sm capitalize", moodLabelClass(meta.mood_label))}>
                    {meta.mood_label}
                  </span>
                ) : null}
              </Stat>
            ) : null}
            {meta.entry_count != null && meta.entry_count > 0 ? (
              <Stat label="Entries"><span className="font-mono text-xl tabular-nums">{meta.entry_count}</span></Stat>
            ) : null}
            {meta.emotions && meta.emotions.length > 0 ? (
              <Stat label="Emotions" className="sm:col-span-2">
                <div className="flex flex-wrap gap-1.5">
                  {meta.emotions.map((e) => (
                    <Pill key={e}>{e}</Pill>
                  ))}
                </div>
              </Stat>
            ) : null}
            {meta.topics && meta.topics.length > 0 ? (
              <Stat label="Topics" className="sm:col-span-2">
                <div className="flex flex-wrap gap-1.5">
                  {meta.topics.map((t) => (
                    <Pill key={t}>{t}</Pill>
                  ))}
                </div>
              </Stat>
            ) : null}
          </CardContent>
        </Card>
      ) : null}

      <Separator />

      <article className="prose-journal max-w-none">
        <ReactMarkdown remarkPlugins={[remarkGfm]}>{data.body}</ReactMarkdown>
      </article>
    </div>
  );
}

function Stat({ label, children, className }: { label: string; children: React.ReactNode; className?: string }) {
  return (
    <div className={cn("space-y-1", className)}>
      <div className="text-[11px] uppercase tracking-wider text-muted-foreground">{label}</div>
      <div>{children}</div>
    </div>
  );
}

function Pill({ children }: { children: React.ReactNode }) {
  return (
    <span className="inline-flex items-center rounded-full border border-border bg-secondary/40 px-2 py-0.5 text-xs capitalize">
      {children}
    </span>
  );
}

function moodLabelClass(label: string): string {
  switch (label.toLowerCase()) {
    case "positive":
      return "text-accent";
    case "negative":
      return "text-destructive";
    default:
      return "text-muted-foreground";
  }
}

function periodHeading(periodType: string, start: string, end: string): string {
  if (periodType === "day") return formatLongDate(start);
  if (periodType === "year") {
    return start.slice(0, 4);
  }
  if (periodType === "month") {
    const [y, m] = start.split("-").map(Number);
    const dt = new Date(Date.UTC(y, m - 1, 1));
    return dt.toLocaleDateString(undefined, { month: "long", year: "numeric", timeZone: "UTC" });
  }
  return `${formatShortDate(start)} – ${formatShortDate(end)}`;
}

function formatLongDate(yyyymmdd: string): string {
  const [y, m, d] = yyyymmdd.split("-").map(Number);
  return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString(undefined, {
    weekday: "long",
    year: "numeric",
    month: "long",
    day: "numeric",
    timeZone: "UTC",
  });
}

function formatShortDate(yyyymmdd: string): string {
  const [y, m, d] = yyyymmdd.split("-").map(Number);
  return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  });
}
