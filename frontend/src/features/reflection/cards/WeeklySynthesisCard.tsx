import { useEffect, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { RefreshCw, Sparkles } from "lucide-react";

import { Button } from "@/components/ui/button";
import { PaperPage, PaperPageProse } from "@/components/ui/paper-page";
import { cn } from "@/lib/utils";

import { useMe } from "@/features/auth/useAuth";
import { SummaryJobStatus } from "@/features/summaries/api";
import { useSummaryJobStatus } from "@/features/summaries/hooks";

import { ReflectionResponse, ReflectionTheme } from "../api";
import { REFLECTION_THIS_WEEK_KEY } from "../hooks";

// WeeklySynthesisCard — the model-written letter that sits above the
// rest of Card 1 (and the Done page). Renders on a single paper sheet
// (<PaperPage>) so the wizard and the Trends-tab letter share the same
// look. The body is structured: a name-aware greeting, four short
// paragraphs (charged / drained / grateful / insights), a closing-
// question pull-quote, and a sign-off. Theme chips render below in their
// own block.
//
// Legacy rows synthesised before the structured prompt landed expose
// only `data.letter` (a single blob); those render as a single paragraph
// to preserve the read.
//
// Renders nothing for legacy weeks that never got synthesis fields
// populated and aren't being backfilled — better silence than empty
// scaffolding.
export function WeeklySynthesisCard({
  data,
  regenerating = false,
  onRegenerate,
}: {
  data: ReflectionResponse;
  /** True while the parent's regenerate mutation is in flight — used to
   * hide the stale body before the job row flips to pending. */
  regenerating?: boolean;
  /** When provided, the empty/terminal state surfaces a "Generate now"
   * action that triggers a fresh job. Omitted by History (past weeks
   * are locked) and LetterCard (wizard just waits). */
  onRegenerate?: () => void;
}) {
  const me = useMe();
  const displayName = me.data?.display_name?.trim() ?? "";
  const greetingName = displayName !== "" ? displayName : "traveler";

  const charged = data.charged?.trim() ?? "";
  const drained = data.drained?.trim() ?? "";
  const grateful = data.grateful?.trim() ?? "";
  const insights = data.insights?.trim() ?? "";
  const legacyLetter = data.letter?.trim() ?? "";
  const closingQuestion = data.closing_question?.trim() ?? "";
  const themes = data.themes ?? [];
  const hasStructured =
    charged !== "" || drained !== "" || grateful !== "" || insights !== "";
  const hasAnyBody = hasStructured || legacyLetter !== "";

  const qc = useQueryClient();
  const jobStatus = useSummaryJobStatus("week", data.week_start);
  const status = jobStatus.data?.status;
  const jobInFlight = status === "pending" || status === "claimed";

  // When the job finishes, pull the fresh reflection so the new letter
  // and theme chips render without the user having to refresh. We watch
  // the status string and invalidate once it drops out of in-flight.
  const [wasInFlight, setWasInFlight] = useState(false);
  useEffect(() => {
    if (jobInFlight) {
      setWasInFlight(true);
      return;
    }
    if (wasInFlight) {
      qc.invalidateQueries({ queryKey: REFLECTION_THIS_WEEK_KEY });
      setWasInFlight(false);
    }
  }, [jobInFlight, wasInFlight, qc]);

  const showBody = hasAnyBody && !regenerating;
  const inFlight = data.synthesis_pending || jobInFlight || regenerating;
  // Empty/terminal state: synthesis is missing AND nothing is on the
  // way. Surfacing this state lets us tell the user *why* (skipped /
  // failed / never queued) instead of lying with "arriving soon".
  const showEmpty = !showBody && !inFlight;

  // Render nothing only when there is genuinely nothing to say AND
  // nowhere for the user to act — i.e. history view on a legacy week
  // with no synthesis. With onRegenerate present, the empty state
  // includes a recovery affordance, so the card stays.
  if (!showBody && !inFlight && !onRegenerate) {
    return null;
  }

  return (
    <PaperPage>
      <PaperPageProse>
        <p>Dear {greetingName},</p>

        {inFlight && !showBody ? <PendingPlaceholder /> : null}
        {showEmpty && onRegenerate ? (
          <EmptyPlaceholder
            status={status}
            regenerating={regenerating}
            onRegenerate={onRegenerate}
          />
        ) : null}

        {showBody && hasStructured ? (
          <>
            <LetterParagraph>{charged}</LetterParagraph>
            <LetterParagraph>{drained}</LetterParagraph>
            <LetterParagraph>{grateful}</LetterParagraph>
            <LetterParagraph>{insights}</LetterParagraph>
          </>
        ) : null}

        {showBody && !hasStructured && legacyLetter !== "" ? (
          <p className="whitespace-pre-wrap">{legacyLetter}</p>
        ) : null}

        {showBody && closingQuestion !== "" ? (
          <p className="border-l-2 border-accent/40 pl-4 italic text-foreground/90">
            {closingQuestion}
          </p>
        ) : null}

        {showBody ? (
          <p className="text-right">— with care, your Journaling Guru</p>
        ) : null}
      </PaperPageProse>

      {themes.length > 0 && !regenerating ? (
        <div className="border-t border-border/40 pt-6">
          <ThemeChips themes={themes} />
        </div>
      ) : null}
    </PaperPage>
  );
}

function LetterParagraph({ children }: { children: ReactNode }) {
  const text = typeof children === "string" ? children.trim() : children;
  if (typeof text === "string" && text === "") return null;
  return <p>{text}</p>;
}

function PendingPlaceholder() {
  return (
    <p className="inline-flex items-center gap-2 text-sm not-italic text-muted-foreground">
      <Sparkles className="h-4 w-4 text-accent/70" />
      <span className="italic">
        Synthesis arriving — usually within a minute of the week closing.
        Pull to refresh.
      </span>
    </p>
  );
}

// EmptyPlaceholder — shown when synthesis is missing AND no in-flight
// job exists to produce one. The copy is keyed off the last job's
// terminal status so users see *why* (skipped vs failed vs never queued)
// instead of a stale "arriving soon" banner.
function EmptyPlaceholder({
  status,
  regenerating,
  onRegenerate,
}: {
  status: SummaryJobStatus | undefined;
  regenerating: boolean;
  onRegenerate: () => void;
}) {
  let copy: string;
  let cta: string;
  if (status === "failed") {
    copy = "We couldn't compose this week's synthesis on the last attempt.";
    cta = "Try again";
  } else if (status === "skipped") {
    copy = "Not enough content this week for a synthesis.";
    cta = "Compose anyway";
  } else if (status === "completed") {
    copy = "The last run finished without writing a synthesis.";
    cta = "Try again";
  } else {
    copy = "No synthesis has been composed for this week yet.";
    cta = "Compose now";
  }
  return (
    <div className="space-y-3 not-italic">
      <p className="inline-flex items-center gap-2 text-sm text-muted-foreground">
        <Sparkles className="h-4 w-4 text-accent/70" />
        <span>{copy}</span>
      </p>
      <Button
        type="button"
        size="sm"
        variant="outline"
        onClick={onRegenerate}
        disabled={regenerating}
        className="gap-1.5"
      >
        <RefreshCw
          className={cn("h-3.5 w-3.5", regenerating && "animate-spin")}
        />
        {regenerating ? "Composing…" : cta}
      </Button>
    </div>
  );
}

function ThemeChips({ themes }: { themes: ReflectionTheme[] }) {
  const [openIdx, setOpenIdx] = useState<number | null>(null);
  return (
    <div className="space-y-3">
      <p className="text-[11px] uppercase tracking-wider text-muted-foreground">
        Themes
      </p>
      <div className="flex flex-wrap gap-2">
        {themes.map((theme, i) => {
          const open = openIdx === i;
          return (
            <button
              key={`${theme.name}-${i}`}
              type="button"
              onClick={() => setOpenIdx(open ? null : i)}
              aria-expanded={open}
              className={cn(
                "inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-xs transition-colors",
                roleClasses(theme.role),
                open && "ring-1 ring-foreground/20",
              )}
            >
              <span className="font-medium">{theme.name}</span>
              <span className="font-mono tabular-nums opacity-70">
                · {theme.days_appeared}d
              </span>
            </button>
          );
        })}
      </div>
      {openIdx !== null && themes[openIdx] ? (
        <ThemeDetail theme={themes[openIdx]} />
      ) : null}
    </div>
  );
}

function ThemeDetail({ theme }: { theme: ReflectionTheme }) {
  return (
    <div className="space-y-1 rounded-md border border-border/60 bg-background/50 px-3 py-2">
      <p className="text-[11px] uppercase tracking-wider text-muted-foreground">
        Includes
      </p>
      <p className="text-sm">
        {theme.tags.map((tag, i) => (
          <span key={tag}>
            {i > 0 ? <span className="text-muted-foreground"> · </span> : null}
            <span className="font-mono text-xs">{tag}</span>
          </span>
        ))}
      </p>
      {theme.note ? (
        <p className="border-l-2 border-accent/40 pl-2 text-xs italic text-muted-foreground">
          {theme.note}
        </p>
      ) : null}
    </div>
  );
}

function roleClasses(role: ReflectionTheme["role"]): string {
  switch (role) {
    case "charger":
      return "border-accent/40 bg-accent/10 text-foreground hover:bg-accent/15";
    case "drainer":
      return "border-destructive/30 bg-destructive/10 text-foreground hover:bg-destructive/15";
    default:
      return "border-border bg-muted text-foreground hover:bg-muted/80";
  }
}
