import { Link, useNavigate, useParams } from "react-router-dom";
import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { PaperPage, PaperPageBlock } from "@/components/ui/paper-page";
import { PullToRefresh } from "@/components/shell/PullToRefresh";
import { SwipeNavigator } from "@/components/shell/SwipeNavigator";
import { cn } from "@/lib/utils";

import { listEntries, type JournalEntry } from "./api";
import {
  useEntries,
  useEntryDates,
  useQuestions,
  useUpdateEntry,
  ENTRY_DATES_KEY,
  entriesKey,
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
  const dates = useEntryDates(180);
  const questions = useQuestions();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { date: rawDate } = useParams();

  const selected = rawDate && DATE_RE.test(rawDate) ? rawDate : null;

  // Garbage in the URL bounces back to the bare /history list.
  useEffect(() => {
    if (rawDate && !selected) navigate("/history", { replace: true });
  }, [rawDate, selected, navigate]);

  const detail = useEntries(selected ?? undefined);

  const dateList = dates.data ?? [];
  const idx = selected ? dateList.findIndex((d) => d.local_date === selected) : -1;
  // dates[] is sorted desc — most recent first. So idx-1 is newer, idx+1 is older.
  const newer = idx > 0 ? dateList[idx - 1].local_date : null;
  const older = idx >= 0 && idx < dateList.length - 1 ? dateList[idx + 1].local_date : null;

  const promptByQuestion = new Map(
    (questions.data ?? []).map((q) => [q.id, q.prompt] as const),
  );

  const onRefresh = async () => {
    await qc.refetchQueries({ queryKey: ENTRY_DATES_KEY });
    if (selected) await qc.refetchQueries({ queryKey: entriesKey(selected) });
  };

  if (dates.isPending) return <p className="text-sm text-muted-foreground">Loading…</p>;
  if (dates.isError)
    return <p className="text-sm text-destructive">Couldn't load history.</p>;

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
          <header className="space-y-1">
            <p className="text-xs uppercase tracking-wider text-muted-foreground">History</p>
            <h1 className="font-serif text-h1">Past entries</h1>
            <p className="text-sm text-muted-foreground">
              {dateList.length === 0
                ? "Nothing yet — write today's entries and they'll appear here tomorrow."
                : `${dateList.length} day${dateList.length === 1 ? "" : "s"} on record. Pick a date to read or edit.`}
            </p>
          </header>

          <div className="grid grid-cols-1 gap-6 md:grid-cols-[16rem,1fr]">
            <nav className="space-y-1">
              {dateList.map((d) => {
                const active = selected === d.local_date;
                return (
                  <Link
                    key={d.local_date}
                    to={active ? "/history" : `/history/${d.local_date}`}
                    className={cn(
                      "block w-full rounded-md border-l-4 border-transparent px-3 py-2 text-left text-sm transition-colors",
                      active
                        ? "border-l-accent bg-secondary/50"
                        : "hover:border-l-border hover:bg-secondary/40",
                    )}
                  >
                    <div className="font-medium">{formatHumanDate(d.local_date)}</div>
                    <div className="font-mono text-xs text-muted-foreground tabular-nums">
                      {d.entry_count} answer{d.entry_count === 1 ? "" : "s"}
                    </div>
                  </Link>
                );
              })}
            </nav>

            <div className="min-h-[8rem]">
              {!selected ? (
                <p className="text-sm text-muted-foreground">
                  Pick a date to read or edit what you wrote.
                </p>
              ) : detail.isPending ? (
                <p className="text-sm text-muted-foreground">Loading…</p>
              ) : detail.isError ? (
                <p className="text-sm text-destructive">
                  Couldn't load entries for {selected}.
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
          </div>
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
