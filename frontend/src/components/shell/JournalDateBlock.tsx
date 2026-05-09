import { useEffect, useState } from "react";

import { formatShortHumanDate } from "@/lib/date";

// JournalDateBlock is the small "where are we in time" header that
// lives at the top of the sidebar / mobile drawer. It shows the live
// wall-clock weekday + date + time (ticking each minute) and, when
// the user's day-start cutoff has shifted today's journal entry to
// yesterday's calendar date, an explicit "Journal for {prev}" hint
// below it. The hint is the load-bearing UX: a 01:30 user needs to
// know they're filing under yesterday, not "today."
//
// `journalDate` is the server-computed local_date of today's entries
// (already accounts for users.day_start_minutes).
export function JournalDateBlock({ journalDate }: { journalDate: string | null }) {
  const now = useNowMinute();
  const wallDate = toLocalISO(now);
  const time = now.toLocaleTimeString(undefined, {
    hour: "numeric",
    minute: "2-digit",
  });
  const wallLabel = `${now.toLocaleDateString(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric",
  })} · ${time}`;
  // Highlight the journal hint when the day-start cutoff has rolled
  // it back to yesterday's calendar date — that's the case the user
  // most needs to notice. Otherwise render it muted, as a quiet
  // confirmation of which day they're filing into.
  const isShifted = journalDate !== null && journalDate !== wallDate;
  return (
    <div className="px-2 pb-3 pt-1 space-y-0.5">
      <p className="font-mono text-xs text-muted-foreground">{wallLabel}</p>
      {journalDate ? (
        <p
          className={
            isShifted
              ? "font-mono text-[11px] text-primary/85"
              : "font-mono text-[11px] text-muted-foreground/80"
          }
          title={
            isShifted
              ? `Past midnight, before your day-start cutoff — this entry files under ${journalDate}.`
              : `This entry files under ${journalDate}.`
          }
        >
          Journal for {formatShortHumanDate(journalDate)}
        </p>
      ) : null}
    </div>
  );
}

// Re-render once per minute. Keyed off Date.now() rounded to minute
// so we don't churn React state every second.
function useNowMinute(): Date {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const tick = () => setNow(new Date());
    // First tick fires at the next minute boundary, then every minute.
    const msToNextMinute = 60_000 - (Date.now() % 60_000);
    let interval: ReturnType<typeof setInterval> | null = null;
    const timeout = setTimeout(() => {
      tick();
      interval = setInterval(tick, 60_000);
    }, msToNextMinute);
    return () => {
      clearTimeout(timeout);
      if (interval) clearInterval(interval);
    };
  }, []);
  return now;
}

function toLocalISO(d: Date): string {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}
