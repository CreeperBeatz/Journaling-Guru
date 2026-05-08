import { Link, useNavigate, useParams } from "react-router-dom";
import { useEffect, useMemo, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { PaperPage, PaperPageBlock } from "@/components/ui/paper-page";
import {
  HeatGrid,
  buildHeatCells,
  type HeatView,
} from "@/components/ui/heat-grid";
import { StreakBadge, computeStreak } from "@/components/ui/streak-badge";
import { PullToRefresh } from "@/components/shell/PullToRefresh";
import { SwipeNavigator } from "@/components/shell/SwipeNavigator";
import { cn } from "@/lib/utils";

import { listEntries, type JournalEntry } from "./api";
import {
  useEntries,
  useEntryDates,
  useHeatmap,
  useQuestions,
  useUpdateEntry,
  ENTRY_DATES_KEY,
  entriesKey,
  heatmapKey,
} from "./hooks";
import { HistoryDailyInputs } from "./HistoryDailyInputs";
import { HistoryChatTranscript } from "@/features/chat/HistoryChatTranscript";

function formatHumanDate(yyyymmdd: string): string {
  const [y, m, d] = yyyymmdd.split("-").map(Number);
  if (!y || !m || !d) return yyyymmdd;
  const date = new Date(Date.UTC(y, m - 1, d));
  return date.toLocaleDateString(undefined, {
    weekday: "long",
    year: "numeric",
    month: "long",
    day: "numeric",
    timeZone: "UTC",
  });
}

const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;

export function HistoryView() {
  const { date: rawDate } = useParams();
  const navigate = useNavigate();
  const selected = rawDate && DATE_RE.test(rawDate) ? rawDate : null;

  // Garbage in the URL bounces back to the bare /history.
  useEffect(() => {
    if (rawDate && !selected) navigate("/history", { replace: true });
  }, [rawDate, selected, navigate]);

  return selected ? <HistoryDetail localDate={selected} /> : <HistoryLanding />;
}

// ---------- Landing (heatmap + recent entries) ----------

const RECENT_PAGE_STEP = 14;

function HistoryLanding() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const heatmap = useHeatmap();
  const [recentLimit, setRecentLimit] = useState(RECENT_PAGE_STEP);
  const dates = useEntryDates(recentLimit);

  // View + anchor drive the drill-down. Year (no anchor = today) →
  // click a month tile → month with that anchor → click a week → week
  // with that anchor → click a day → /history/:date.
  const [view, setView] = useState<HeatView>("year");
  const [anchor, setAnchor] = useState<string | null>(null);

  const days = heatmap.data?.days ?? [];
  const today = heatmap.data?.today ?? new Date().toISOString().slice(0, 10);
  const cells = useMemo(() => buildHeatCells(days), [days]);
  const streak = useMemo(() => computeStreak(cells, today), [cells, today]);

  const onRefresh = async () => {
    await Promise.all([
      qc.refetchQueries({ queryKey: heatmapKey() }),
      qc.refetchQueries({ queryKey: ENTRY_DATES_KEY }),
    ]);
  };

  // ViewToggle changes always reset the anchor so the user lands on
  // today's slice of whichever view they picked.
  const setViewReset = (v: HeatView) => {
    setView(v);
    setAnchor(null);
  };

  const goUp = () => {
    if (view === "week") {
      setView("month");
      // keep anchor so the parent month is highlighted
    } else if (view === "month") {
      setView("year");
      setAnchor(null);
    }
  };

  return (
    <PullToRefresh onRefresh={onRefresh}>
      <div className="space-y-8">
        <header className="flex flex-wrap items-end justify-between gap-3">
          <div className="space-y-1">
            <p className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground">
              History
            </p>
            <h1 className="font-serif text-h1 leading-tight">Where you&apos;ve been</h1>
          </div>
          <StreakBadge days={streak} />
        </header>

        <section className="space-y-4">
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-2">
              <ViewToggle value={view} onChange={setViewReset} />
              {view !== "year" ? (
                <button
                  type="button"
                  onClick={goUp}
                  className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground hover:text-foreground"
                >
                  ← Up
                </button>
              ) : null}
            </div>
            <p className="font-mono text-[11px] text-muted-foreground" aria-hidden>
              none · short · mid · deep
            </p>
          </div>

          {heatmap.isPending ? (
            <p className="text-sm text-muted-foreground">Loading heatmap…</p>
          ) : heatmap.isError ? (
            <p className="text-sm text-destructive">Couldn&apos;t load heatmap.</p>
          ) : (
            <HeatGrid
              cells={cells}
              view={view}
              anchor={anchor ?? today}
              onMonthClick={(iso) => {
                setAnchor(iso);
                setView("month");
              }}
              onWeekClick={(iso) => {
                setAnchor(iso);
                setView("week");
              }}
              onSelect={(iso) => {
                qc.prefetchQuery({
                  queryKey: entriesKey(iso),
                  queryFn: () => listEntries(iso),
                });
                navigate(`/history/${iso}`);
              }}
            />
          )}
        </section>

        <section className="space-y-3">
          <h2 className="font-serif text-h2">Recent entries</h2>
          {dates.isPending ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : dates.isError ? (
            <p className="text-sm text-destructive">Couldn&apos;t load history.</p>
          ) : dates.data!.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              Nothing yet — write today&apos;s entries and they&apos;ll appear here tomorrow.
            </p>
          ) : (
            <>
              <ul className="divide-y divide-border/60">
                {dates.data!.map((d) => (
                  <li key={d.local_date}>
                    <Link
                      to={`/history/${d.local_date}`}
                      className="flex items-baseline justify-between gap-3 py-3 transition-colors hover:bg-secondary/30 -mx-2 px-2 rounded-md"
                    >
                      <span className="font-medium">{formatHumanDate(d.local_date)}</span>
                      <span className="font-mono text-xs tabular-nums text-muted-foreground">
                        {d.entry_count} answer{d.entry_count === 1 ? "" : "s"}
                      </span>
                    </Link>
                  </li>
                ))}
              </ul>
              {/* Pagination: only show "Load more" when the page is full.
                  When the API returns fewer rows than asked for, we know
                  we've hit the end and the button can be hidden. */}
              {dates.data!.length >= recentLimit ? (
                <div className="flex justify-center pt-1">
                  <button
                    type="button"
                    onClick={() => setRecentLimit((n) => n + RECENT_PAGE_STEP)}
                    className={cn(
                      "rounded-md px-3 py-1.5 text-sm",
                      "text-muted-foreground transition-colors hover:text-foreground hover:bg-secondary/60",
                      "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                    )}
                  >
                    Load more
                  </button>
                </div>
              ) : null}
            </>
          )}
        </section>
      </div>
    </PullToRefresh>
  );
}

function ViewToggle({
  value,
  onChange,
}: {
  value: HeatView;
  onChange: (v: HeatView) => void;
}) {
  const opts: { v: HeatView; label: string }[] = [
    { v: "year", label: "Year" },
    { v: "month", label: "Month" },
    { v: "week", label: "Week" },
  ];
  return (
    <div className="inline-flex rounded-md border border-border bg-card p-0.5 text-xs">
      {opts.map((o) => (
        <button
          key={o.v}
          type="button"
          onClick={() => onChange(o.v)}
          className={cn(
            "rounded px-3 py-1 transition-colors",
            value === o.v
              ? "bg-secondary text-foreground"
              : "text-muted-foreground hover:text-foreground",
          )}
          aria-pressed={value === o.v}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

// ---------- Detail (single day) ----------

function HistoryDetail({ localDate }: { localDate: string }) {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const detail = useEntries(localDate);
  const dates = useEntryDates(180);
  const questions = useQuestions();

  const dateList = dates.data ?? [];
  const idx = dateList.findIndex((d) => d.local_date === localDate);
  const newer = idx > 0 ? dateList[idx - 1].local_date : null;
  const older = idx >= 0 && idx < dateList.length - 1 ? dateList[idx + 1].local_date : null;

  const promptByQuestion = new Map(
    (questions.data ?? []).map((q) => [q.id, q.prompt] as const),
  );

  const onRefresh = async () => {
    await qc.refetchQueries({ queryKey: entriesKey(localDate) });
  };

  return (
    <PullToRefresh onRefresh={onRefresh}>
      <SwipeNavigator
        onSwipeRight={() => {
          if (older) navigate(`/history/${older}`);
        }}
        onSwipeLeft={() => {
          if (newer) navigate(`/history/${newer}`);
        }}
        onDragStart={() => {
          if (older) {
            qc.prefetchQuery({ queryKey: entriesKey(older), queryFn: () => listEntries(older) });
          }
          if (newer) {
            qc.prefetchQuery({ queryKey: entriesKey(newer), queryFn: () => listEntries(newer) });
          }
        }}
      >
        <div className="space-y-6">
          <Link
            to="/history"
            className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground hover:text-foreground"
          >
            ← Back to overview
          </Link>

          {detail.isPending ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : detail.isError ? (
            <p className="text-sm text-destructive">
              Couldn&apos;t load entries for {localDate}.
            </p>
          ) : (
            <div className="space-y-6">
              <HistoryDailyInputs localDate={detail.data!.local_date} />
              <HistoryChatTranscript localDate={detail.data!.local_date} />
              {detail.data!.entries.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  No entries on this date.
                </p>
              ) : (
                <HistoryPaperPage
                  localDate={detail.data!.local_date}
                  entries={detail.data!.entries}
                  promptByQuestion={promptByQuestion}
                  dateLabel={formatHumanDate(detail.data!.local_date)}
                />
              )}
            </div>
          )}
        </div>
      </SwipeNavigator>
    </PullToRefresh>
  );
}

interface HistoryPaperPageProps {
  localDate: string;
  entries: JournalEntry[];
  promptByQuestion: Map<string, string>;
  dateLabel: string;
}

function HistoryPaperPage({
  localDate,
  entries,
  promptByQuestion,
  dateLabel,
}: HistoryPaperPageProps) {
  const update = useUpdateEntry(localDate);
  return (
    <PaperPage eyebrow="Entry" title={dateLabel}>
      {entries.map((e) => (
        <PaperPageBlock
          key={e.id}
          prompt={promptByQuestion.get(e.question_id) ?? "Question"}
          initialBody={e.body}
          onSave={(body) => update.mutate({ id: e.id, body })}
          saving={update.isPending}
          placeholder="Empty to delete this entry."
        />
      ))}
    </PaperPage>
  );
}
