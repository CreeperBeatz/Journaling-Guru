import { useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useNavigate, useParams } from "react-router-dom";
import { AlertCircle, ArrowLeft, Loader2, RefreshCw } from "lucide-react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { toast } from "@/components/ui/sonner";
import { cn } from "@/lib/utils";

import type { SummaryJob } from "./api";
import {
  SUMMARY_KEY,
  useRegenerateSummary,
  useSummary,
  useSummaryJobStatus,
} from "./hooks";

const periodLabel: Record<string, string> = {
  day: "Daily",
  week: "Weekly",
  month: "Monthly",
  year: "Yearly",
};

export function SummaryDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { data, isPending, isError } = useSummary(id);
  const regen = useRegenerateSummary();
  const jobQuery = useSummaryJobStatus(data?.period_type, data?.period_start);
  const job = jobQuery.data ?? null;
  const inFlight = job?.status === "pending" || job?.status === "claimed";

  // Detect lifecycle transitions on the polled job and side-effect:
  // pending|claimed → completed: refetch the summary so the new body
  // replaces the stale one, plus toast for confirmation. → failed:
  // toast the error so the user knows the regen really didn't land.
  // Skipped is silent — the body didn't change.
  const prevStatusRef = useRef<SummaryJob["status"] | undefined>(undefined);
  useEffect(() => {
    const prev = prevStatusRef.current;
    const next = job?.status;
    prevStatusRef.current = next;
    if (!prev || !next || prev === next) return;
    const wasInFlight = prev === "pending" || prev === "claimed";
    if (!wasInFlight) return;
    if (next === "completed") {
      if (id) qc.invalidateQueries({ queryKey: SUMMARY_KEY(id) });
      qc.invalidateQueries({ queryKey: ["summaries", "list"] });
      qc.invalidateQueries({ queryKey: ["summaries", "stats"] });
      toast.success("Regeneration complete", {
        description: "The new draft is loaded below.",
      });
    } else if (next === "failed") {
      toast.error("Regeneration failed", {
        description: job?.last_error ?? "The worker exhausted retries.",
      });
    }
  }, [job?.status, job?.last_error, id, qc]);

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
  const showFailed = job?.status === "failed";
  const buttonDisabled = regen.isPending || inFlight;

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-3">
        <Button variant="ghost" size="sm" onClick={() => navigate(-1)}>
          <ArrowLeft className="h-4 w-4" /> Back
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={buttonDisabled}
          onClick={() =>
            regen.mutate({
              period_type: data.period_type,
              period_start: data.period_start,
            })
          }
        >
          {buttonDisabled ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <RefreshCw className="h-4 w-4" />
          )}
          {inFlight ? "Regenerating…" : "Regenerate"}
        </Button>
      </div>

      {inFlight ? <RegenBanner job={job!} /> : null}
      {showFailed ? <FailedBanner job={job!} /> : null}

      <header className="space-y-1">
        <p className="text-xs uppercase tracking-wider text-muted-foreground">
          {periodLabel[data.period_type] ?? data.period_type} reflection
        </p>
        <h1 className="font-serif text-h1">{periodHeading(data.period_type, data.period_start, data.period_end)}</h1>
        <p className="text-xs font-mono text-muted-foreground tabular-nums">
          generated {generated.toLocaleString()} · {data.model}
          {inFlight ? <span className="ml-2 italic">(showing previous draft)</span> : null}
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

      <article
        aria-busy={inFlight || undefined}
        className={cn(
          "prose-journal max-w-none transition-opacity duration-300",
          inFlight && "opacity-60",
        )}
      >
        <ReactMarkdown remarkPlugins={[remarkGfm]}>{data.body}</ReactMarkdown>
      </article>
    </div>
  );
}

// RegenBanner is shown while the queue row is pending or claimed. The
// status-specific copy lets the user distinguish "queued" (worker not
// yet assigned) from "running" (worker has it) — useful for confirming
// the dispatcher isn't stuck.
function RegenBanner({ job }: { job: SummaryJob }) {
  const queued = job.status === "pending";
  return (
    <div
      role="status"
      aria-live="polite"
      className={cn(
        "flex items-start gap-3 rounded-lg border border-accent/30 bg-accent/10 p-3 text-sm",
      )}
    >
      <Loader2 className="mt-0.5 h-4 w-4 shrink-0 animate-spin text-accent" />
      <div className="space-y-0.5">
        <p className="font-medium">
          {queued ? "Queued for regeneration" : "Regenerating now"}
          {job.attempts > 1 ? (
            <span className="ml-2 text-xs font-mono text-muted-foreground tabular-nums">
              attempt {job.attempts}
            </span>
          ) : null}
        </p>
        <p className="text-xs text-muted-foreground">
          {queued
            ? "Waiting for the worker to pick it up — the dispatcher polls every few seconds."
            : "The worker is calling the model. The previous draft is shown below; this page will refresh when the new one lands."}
        </p>
      </div>
    </div>
  );
}

function FailedBanner({ job }: { job: SummaryJob }) {
  return (
    <div
      role="alert"
      className="flex items-start gap-3 rounded-lg border border-destructive/40 bg-destructive/5 p-3 text-sm"
    >
      <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
      <div className="space-y-0.5">
        <p className="font-medium text-destructive">Last regeneration failed</p>
        <p className="text-xs text-muted-foreground break-words">
          {job.last_error ?? "The worker exhausted retries."} Try again — the
          previous draft below is still intact.
        </p>
      </div>
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
