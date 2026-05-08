import type { ReactNode } from "react";

import { cn } from "@/lib/utils";

export interface GuruNoteProps {
  children: ReactNode;
  /** Optional small label above the body ("Noticed", "From the guru"…). */
  eyebrow?: string;
  className?: string;
}

/**
 * Accent-bordered narrative callout. Used on the Summary dashboard and
 * (when an entry has a guru note) on /history/:date. Italic serif on
 * a 4px accent margin-bar — the bar IS the boundary, so don't nest
 * inside another bordered <Card>.
 */
export function GuruNote({ children, eyebrow, className }: GuruNoteProps) {
  return (
    <blockquote
      className={cn(
        "border-l-4 border-accent pl-4 py-2 italic text-foreground/90 font-serif",
        className,
      )}
    >
      {eyebrow ? (
        <p className="not-italic font-sans font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground mb-1">
          {eyebrow}
        </p>
      ) : null}
      <div className="leading-prose text-body-prose">{children}</div>
    </blockquote>
  );
}
