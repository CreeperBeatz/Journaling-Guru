import { useState, type ReactNode } from "react";
import { Loader2 } from "lucide-react";

import { Textarea } from "@/components/ui/textarea";
import { useDebouncedFlag } from "@/lib/useDebouncedFlag";
import { cn } from "@/lib/utils";

export interface PaperPageProps {
  /** Small uppercase label above the date (e.g. "Today", "A letter from your guru"). */
  eyebrow?: string;
  /** Display title on the sheet — typically the human-readable date. */
  title: ReactNode;
  /** Optional sub-line below the title (clock, timezone, etc.). */
  meta?: ReactNode;
  /** Slot rendered next to the title — status pill, action buttons, etc. */
  headerSlot?: ReactNode;
  children: ReactNode;
  className?: string;
}

/**
 * Single-sheet "paper" surface. Reused by:
 *   - Manual flow completion (Today, all questions answered)
 *   - History entry detail (`/history/:date`)
 *   - Weekly letter on `/summary`
 *
 * Brighter than `--card` (uses `--paper-sheet`); `shadow-md` lift in light
 * mode. Save-on-blur lives on the children (PaperPageBlock owns it).
 */
export function PaperPage({
  eyebrow,
  title,
  meta,
  headerSlot,
  children,
  className,
}: PaperPageProps) {
  return (
    <article
      className={cn(
        "mx-auto w-full max-w-3xl rounded-2xl bg-paper-sheet shadow-md ring-1 ring-border/50",
        "px-6 py-8 sm:px-12 sm:py-12",
        className,
      )}
    >
      <header className="mb-8 flex items-start justify-between gap-3 border-l-4 border-accent pl-4">
        <div className="space-y-1">
          {eyebrow ? (
            <p className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground">
              {eyebrow}
            </p>
          ) : null}
          <h1 className="font-serif text-h1 leading-tight">{title}</h1>
          {meta ? (
            <p className="font-mono text-xs tabular-nums text-muted-foreground">
              {meta}
            </p>
          ) : null}
        </div>
        {headerSlot ? <div className="shrink-0">{headerSlot}</div> : null}
      </header>

      <div className="space-y-8">{children}</div>
    </article>
  );
}

export interface PaperPageBlockProps {
  prompt: string;
  initialBody: string;
  /** Called on blur when the local draft differs from the last server body. */
  onSave: (body: string) => void;
  /** True while the parent's mutation is in-flight (drives the spinner). */
  saving?: boolean;
  placeholder?: string;
}

/**
 * One question/answer pair inside an entry-variant <PaperPage>. Save-on-blur
 * is the contract: parent wires the mutation, this block tracks dirty
 * state and emits onSave when focus leaves with unflushed edits. Reflects
 * server pushes via the `initialBody` prop, but only when the user hasn't
 * diverged locally.
 */
export function PaperPageBlock({
  prompt,
  initialBody,
  onSave,
  saving = false,
  placeholder = "Write whatever comes to mind…",
}: PaperPageBlockProps) {
  const [lastServerBody, setLastServerBody] = useState(initialBody);
  const [draft, setDraft] = useState(initialBody);

  // Pull server changes into the local draft when the user hasn't
  // diverged.
  if (initialBody !== lastServerBody) {
    if (draft === lastServerBody) setDraft(initialBody);
    setLastServerBody(initialBody);
  }

  const dirty = draft !== initialBody;
  const showSpinner = useDebouncedFlag(saving, 300);

  const handleBlur = () => {
    if (!dirty) return;
    onSave(draft);
  };

  return (
    <section className="group space-y-3">
      <header className="flex items-baseline justify-between gap-3">
        <h3 className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground">
          {prompt}
        </h3>
        {showSpinner ? (
          <Loader2
            className="h-3 w-3 animate-spin text-muted-foreground"
            aria-label="Saving"
          />
        ) : null}
      </header>
      <Textarea
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={handleBlur}
        placeholder={placeholder}
        rows={3}
        className={cn(
          "min-h-[3.5rem] border-transparent bg-transparent px-0 leading-prose text-body",
          "focus-visible:ring-0 focus-visible:ring-offset-0",
          "focus-visible:border-b-border focus-visible:border-b rounded-none",
          // Empty-state affordance: keep the answer line visible even
          // when the body is empty so the page reads as a structured
          // sheet, not a stack of disappearing rows.
          "data-[empty=true]:border-b-border data-[empty=true]:border-b",
        )}
        data-empty={draft.trim() === "" ? "true" : undefined}
      />
    </section>
  );
}

/**
 * Read-only prose body for the letter variant. Keep the same paper sheet
 * surface, drop the per-block prompt structure.
 */
export function PaperPageProse({ children }: { children: ReactNode }) {
  return (
    <div className="prose-paper space-y-5 font-serif text-body-prose leading-prose">
      {children}
    </div>
  );
}

