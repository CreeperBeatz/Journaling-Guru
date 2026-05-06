import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { PullToRefresh } from "@/components/shell/PullToRefresh";
import { SwipeNavigator } from "@/components/shell/SwipeNavigator";
import { useMe } from "@/features/auth/useAuth";
import { minutesToHHMM } from "@/lib/dayStart";

import { listEntries } from "./api";
import { useEntries, useQuestions, ENTRY_DATES_KEY, entriesKey } from "./hooks";
import { QuestionAnswer } from "./QuestionAnswer";
import { DailyInputs } from "@/features/daily/DailyInputs";
import { useDailyInput, useSaveDailyInput } from "@/features/daily/hooks";

// useNow ticks every 30s — fine-grained enough that the clock visibly
// matches the wall and the rollover boundary appears live, without
// re-rendering the page every second.
function useNow(): Date {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const t = setInterval(() => setNow(new Date()), 30_000);
    return () => clearInterval(t);
  }, []);
  return now;
}

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

// Calendar arithmetic in UTC space — the local_date is a wall-clock day so
// "the day before" is a calendar subtraction, not a timezone shift.
function dayBefore(yyyymmdd: string): string | null {
  const [y, m, d] = yyyymmdd.split("-").map(Number);
  if (!y || !m || !d) return null;
  const date = new Date(Date.UTC(y, m - 1, d));
  date.setUTCDate(date.getUTCDate() - 1);
  const yy = date.getUTCFullYear();
  const mm = String(date.getUTCMonth() + 1).padStart(2, "0");
  const dd = String(date.getUTCDate()).padStart(2, "0");
  return `${yy}-${mm}-${dd}`;
}

export function DailyEntry() {
  const me = useMe();
  const questions = useQuestions();
  const entries = useEntries();
  const dailyInput = useDailyInput();
  const saveDaily = useSaveDailyInput();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const now = useNow();

  if (questions.isPending || entries.isPending) {
    return <p className="text-sm text-muted-foreground">Loading…</p>;
  }
  if (questions.isError) {
    return <p className="text-sm text-destructive">Couldn't load questions.</p>;
  }
  if (entries.isError) {
    return <p className="text-sm text-destructive">Couldn't load today's entries.</p>;
  }

  const today = entries.data?.local_date;
  const yesterday = today ? dayBefore(today) : null;
  const dayLabel = today ? formatHumanDate(today) : "Today";

  const entryByQuestion = new Map(
    (entries.data?.entries ?? []).map((e) => [e.question_id, e]),
  );
  // Render the clock in the user's stored timezone so it matches the
  // calendar date logic — never the browser's TZ.
  const currentTime = me.data
    ? new Intl.DateTimeFormat(undefined, {
        hour: "2-digit",
        minute: "2-digit",
        hour12: false,
        timeZone: me.data.timezone,
      }).format(now)
    : null;

  const onRefresh = async () => {
    await Promise.all([
      qc.refetchQueries({ queryKey: entriesKey() }),
      qc.refetchQueries({ queryKey: ENTRY_DATES_KEY }),
    ]);
  };

  return (
    <PullToRefresh onRefresh={onRefresh}>
      <SwipeNavigator
        onSwipeRight={() => {
          if (yesterday) navigate(`/history/${yesterday}`);
        }}
        onDragStart={() => {
          if (yesterday) {
            qc.prefetchQuery({
              queryKey: entriesKey(yesterday),
              queryFn: () => listEntries(yesterday),
            });
          }
        }}
      >
        <div className="space-y-6">
          <header className="space-y-1">
            <p className="text-xs uppercase tracking-wider text-muted-foreground">Today</p>
            <h1 className="font-serif text-h1">{dayLabel}</h1>
            {me.data ? (
              <p className="font-mono text-xs tabular-nums text-muted-foreground">
                <span>{currentTime}</span> · {me.data.timezone} · new day at{" "}
                {minutesToHHMM(me.data.day_start_minutes)}
              </p>
            ) : null}
          </header>

          <DailyInputs
            input={dailyInput.data?.input ?? null}
            onSave={(body) => saveDaily.mutate(body)}
            isSaving={saveDaily.isPending}
          />

          {questions.data && questions.data.length === 0 ? (
            <Card>
              <CardHeader>
                <CardTitle className="font-serif">No questions yet</CardTitle>
                <CardDescription>
                  You haven't set up any prompts. Add some to start journaling.
                </CardDescription>
              </CardHeader>
              <CardContent>
                <Button asChild>
                  <Link to="/settings?tab=questions">Manage questions</Link>
                </Button>
              </CardContent>
            </Card>
          ) : (
            <div className="space-y-5">
              {(questions.data ?? []).map((q) => (
                <QuestionAnswer
                  key={q.id}
                  question={q}
                  entry={entryByQuestion.get(q.id)}
                />
              ))}
            </div>
          )}
        </div>
      </SwipeNavigator>
    </PullToRefresh>
  );
}
