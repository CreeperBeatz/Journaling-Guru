import { Link } from "react-router-dom";
import { Volume2 } from "lucide-react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

import { PaperPage, PaperPageProse } from "@/components/ui/paper-page";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

import { useMe } from "@/features/auth/useAuth";
import type { Summary } from "@/features/summaries/api";

export interface WeeklyLetterProps {
  summary: Summary | null;
  loading?: boolean;
}

function formatRange(start: string, end: string): string {
  const fmt = (s: string) => {
    const [y, m, d] = s.split("-").map(Number);
    return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString(undefined, {
      month: "short",
      day: "numeric",
      timeZone: "UTC",
    });
  };
  return `${fmt(start)} – ${fmt(end)}`;
}

function isoRange(start: string, end: string): string[] {
  const out: string[] = [];
  const [sy, sm, sd] = start.split("-").map(Number);
  const [ey, em, ed] = end.split("-").map(Number);
  const cur = new Date(Date.UTC(sy, sm - 1, sd));
  const last = new Date(Date.UTC(ey, em - 1, ed));
  while (cur.getTime() <= last.getTime() && out.length < 14) {
    const y = cur.getUTCFullYear();
    const m = String(cur.getUTCMonth() + 1).padStart(2, "0");
    const d = String(cur.getUTCDate()).padStart(2, "0");
    out.push(`${y}-${m}-${d}`);
    cur.setUTCDate(cur.getUTCDate() + 1);
  }
  return out;
}

function dayChipLabel(iso: string): string {
  const [y, m, d] = iso.split("-").map(Number);
  return new Date(Date.UTC(y, m - 1, d)).toLocaleDateString(undefined, {
    weekday: "short",
    timeZone: "UTC",
  });
}

/**
 * Hero of the Trends tab. Renders the most recent weekly summary as a
 * letter on a paper sheet. The structured-prompt path (Sonnet) emits
 * four paragraphs (charged/drained/grateful/insights) + a closing-
 * question pull-quote; the legacy path falls back to the markdown body.
 * Greeting uses the user's display_name when set ("Dear traveler,"
 * otherwise). Footer chips deep-link to each day in the week.
 * Read-aloud is reserved for Phase 6b voice; renders disabled.
 */
export function WeeklyLetter({ summary, loading }: WeeklyLetterProps) {
  const me = useMe();
  const displayName = me.data?.display_name?.trim() ?? "";
  const greetingName = displayName !== "" ? displayName : "traveler";

  if (loading) {
    return (
      <PaperPage
        eyebrow="A letter from your guru"
        title="…"
        meta="loading"
      >
        <p className="text-sm text-muted-foreground">Composing this week&apos;s letter…</p>
        <div className="mt-4 space-y-5" aria-hidden="true">
          {[0, 1, 2].map((para) => (
            <div key={para} className="space-y-2">
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-4 w-11/12" />
              <Skeleton className="h-4 w-4/5" />
            </div>
          ))}
        </div>
      </PaperPage>
    );
  }

  if (!summary) {
    return (
      <PaperPage
        eyebrow="A letter from your guru"
        title="No letter yet"
      >
        <PaperPageProse>
          <p>Dear {greetingName},</p>
          <p>
            Your first letter arrives Sunday morning. Until then, keep showing
            up — the cards, the chat, even just the mood. The page fills
            itself.
          </p>
          <p className="text-right">— with care, your Journaling Guru</p>
        </PaperPageProse>
      </PaperPage>
    );
  }

  const days = isoRange(summary.period_start, summary.period_end);

  const charged = summary.metadata?.charged?.trim() ?? "";
  const drained = summary.metadata?.drained?.trim() ?? "";
  const grateful = summary.metadata?.grateful?.trim() ?? "";
  const insights = summary.metadata?.insights?.trim() ?? "";
  const closingQuestion = summary.metadata?.closing_question?.trim() ?? "";
  const hasStructured =
    charged !== "" || drained !== "" || grateful !== "" || insights !== "";

  return (
    <PaperPage
      eyebrow="A letter from your guru"
      title={`Week of ${formatRange(summary.period_start, summary.period_end)}`}
      headerSlot={
        <button
          type="button"
          disabled
          title="Read aloud arrives with voice mode"
          className={cn(
            "inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs",
            "bg-muted text-muted-foreground opacity-60 cursor-not-allowed",
          )}
        >
          <Volume2 className="h-3.5 w-3.5" aria-hidden />
          Read aloud
          <span className="rounded-full bg-muted-foreground/15 px-1.5 py-0.5 text-[10px] uppercase tracking-wide">
            soon
          </span>
        </button>
      }
    >
      <PaperPageProse>
        <p>Dear {greetingName},</p>

        {hasStructured ? (
          <>
            {charged !== "" ? <p>{charged}</p> : null}
            {drained !== "" ? <p>{drained}</p> : null}
            {grateful !== "" ? <p>{grateful}</p> : null}
            {insights !== "" ? <p>{insights}</p> : null}
            {closingQuestion !== "" ? (
              <p className="border-l-2 border-accent/40 pl-4 italic text-foreground/90">
                {closingQuestion}
              </p>
            ) : null}
          </>
        ) : (
          <ReactMarkdown remarkPlugins={[remarkGfm]}>{summary.body}</ReactMarkdown>
        )}

        <p className="text-right">— with care,</p>
      </PaperPageProse>

      <footer className="mt-2 border-t border-border/60 pt-4">
        <p className="mb-2 font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground">
          Jump to a day
        </p>
        <ul className="flex flex-wrap gap-1.5">
          {days.map((iso) => (
            <li key={iso}>
              <Link
                to={`/history/${iso}`}
                className={cn(
                  "inline-flex items-center gap-1.5 rounded-full px-3 py-1 text-xs",
                  "bg-muted text-muted-foreground transition-colors hover:bg-accent/15 hover:text-accent",
                )}
              >
                <span className="font-mono uppercase tracking-wide">
                  {dayChipLabel(iso)}
                </span>
                <span className="font-mono tabular-nums">
                  {iso.slice(8)}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      </footer>
    </PaperPage>
  );
}
