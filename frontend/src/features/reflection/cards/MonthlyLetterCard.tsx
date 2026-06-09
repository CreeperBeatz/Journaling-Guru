import { Sparkles } from "lucide-react";

import { PaperPage, PaperPageProse } from "@/components/ui/paper-page";

import { useMe } from "@/features/auth/useAuth";

import type { MonthlyReflectionBlock } from "../api";

function monthLabel(monthStart: string): string {
  const [y, m] = monthStart.split("-").map(Number);
  return new Date(Date.UTC(y, m - 1, 1)).toLocaleDateString(undefined, {
    month: "long",
    year: "numeric",
    timeZone: "UTC",
  });
}

// MonthlyLetterCard — the second sheet of a monthly week's reading
// step (and the month section of the Summary tab). Same warm-paper
// treatment as the weekly letter, but the paragraphs sit a month up:
// the arc of the month, what kept showing up across weeks, the goals
// retrospective, and the direction question as the pull-quote.
//
// While the synthesis is still being written (the monthly job fires a
// few minutes after the final weekly letter) an "arriving" state
// renders — the user can continue regardless; the letter also lives in
// the Summary tab once it lands.
export function MonthlyLetterCard({ monthly }: { monthly: MonthlyReflectionBlock }) {
  const me = useMe();
  const displayName = me.data?.display_name?.trim() ?? "";
  const greetingName = displayName !== "" ? displayName : "traveler";

  const arc = monthly.arc.trim();
  const recurring = monthly.recurring.trim();
  const goalsRetro = monthly.goals_retro.trim();
  const closingQuestion = monthly.closing_question.trim();
  const hasBody = arc !== "" || recurring !== "" || goalsRetro !== "";

  return (
    <PaperPage
      eyebrow="Your month, looking back"
      title={monthLabel(monthly.month_start)}
      meta={`${monthly.month_start} → ${monthly.month_end}`}
    >
      <PaperPageProse>
        <p>Dear {greetingName},</p>

        {hasBody ? (
          <>
            {arc !== "" ? <p>{arc}</p> : null}
            {recurring !== "" ? <p>{recurring}</p> : null}
            {goalsRetro !== "" ? <p>{goalsRetro}</p> : null}
            {closingQuestion !== "" ? (
              <p className="border-l-2 border-accent/40 pl-4 italic text-foreground/90">
                {closingQuestion}
              </p>
            ) : null}
            <p className="text-right">— with care, your Journaling Guru</p>
          </>
        ) : (
          <p className="inline-flex items-center gap-2 text-sm not-italic text-muted-foreground">
            <Sparkles className="h-4 w-4 text-accent/70" />
            <span className="italic">
              {monthly.synthesis_pending
                ? "Your monthly letter is being written — it usually lands a few minutes after the weekly one. It'll be waiting on the Summary tab."
                : "No monthly letter was composed for this month — the weeks were too quiet to draw a thread through."}
            </span>
          </p>
        )}
      </PaperPageProse>
    </PaperPage>
  );
}
