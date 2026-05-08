import { useEffect, useMemo, useState, type ReactNode } from "react";
import { ChevronLeft } from "lucide-react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";

import { Card, CardContent } from "@/components/ui/card";
import { cn } from "@/lib/utils";

/** API the active slot's render function receives. */
export interface CardSlotApi {
  /** Advance to the next slot (or fire onComplete if last). Marks the
   *  stack busy until the optional returned promise resolves. */
  advance: () => void;
  /** Back to the previous slot. No-op on the first slot. */
  back: () => void;
  index: number;
  total: number;
  isLast: boolean;
  busy: boolean;
}

export interface CardStackSlot {
  /** Stable id for keying. */
  id: string;
  /** Whether this slot already has a value worth keeping. Drives
   *  `firstEmptyIndex` so users land on the next thing to fill, not
   *  back at slot 0. Defaults to false (treated as empty). */
  hasValue?: boolean;
  /** Renders the slot body. Use the api to advance/back/submit. */
  render: (api: CardSlotApi) => ReactNode;
}

export interface CardStackProps {
  slots: CardStackSlot[];
  /** Fired when the user advances past the last slot, OR clicks the
   *  "show full page" affordance. The parent flips its surface to
   *  PaperPage. */
  onComplete?: () => void;
  startIndex?: number;
  className?: string;
}

const SPRING = { type: "spring", stiffness: 380, damping: 30 } as const;

function firstEmptyIndex(slots: CardStackSlot[]): number {
  const idx = slots.findIndex((s) => !s.hasValue);
  return idx === -1 ? 0 : idx;
}

/**
 * One slot per "card", full focus. Slots own their own input control
 * (textarea, mood slider, emotions chips, etc.) and call `advance()`
 * when ready. Swipe-back / ChevronLeft revisits the previous slot.
 *
 * "Show full page" in the header jumps directly to the parent's
 * complete state without walking through every slot.
 */
export function CardStack({
  slots,
  onComplete,
  startIndex,
  className,
}: CardStackProps) {
  const total = slots.length;
  const initial = useMemo(
    () => (typeof startIndex === "number" ? startIndex : firstEmptyIndex(slots)),
    // re-resolving mid-flow would yank a user off their current card;
    // re-compute only when the count changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [total],
  );
  const [index, setIndex] = useState(initial);
  const [direction, setDirection] = useState<1 | -1>(1);
  const [busy, setBusy] = useState(false);
  const reduceMotion = useReducedMotion();

  useEffect(() => {
    if (index >= total && total > 0) setIndex(total - 1);
  }, [index, total]);

  if (total === 0) return null;
  const slot = slots[index];
  if (!slot) return null;
  const isLast = index === total - 1;

  const advance = () => {
    if (busy) return;
    if (isLast) {
      onComplete?.();
      return;
    }
    setDirection(1);
    setIndex((i) => Math.min(i + 1, total - 1));
  };

  const back = () => {
    if (index === 0 || busy) return;
    setDirection(-1);
    setIndex((i) => i - 1);
  };

  const api: CardSlotApi = {
    advance,
    back,
    index,
    total,
    isLast,
    busy,
  };
  // setBusy is exposed via closure — slots can't toggle it directly,
  // and currently no slot needs to. Keep the field for parity with the
  // earlier API; reintroduce a setter on the api when a slot needs
  // to lock the stack during an async commit.
  void setBusy;

  const variants = reduceMotion
    ? {
        enter: { opacity: 0 },
        center: { opacity: 1 },
        exit: { opacity: 0 },
      }
    : {
        enter: (d: 1 | -1) => ({ x: d * 32, opacity: 0 }),
        center: { x: 0, opacity: 1 },
        exit: (d: 1 | -1) => ({ x: -d * 32, opacity: 0 }),
      };

  return (
    <div className={cn("relative", className)}>
      <div className="flex items-center justify-between gap-3">
        <ProgressBar value={(index + 1) / total} />
        {onComplete ? (
          <button
            type="button"
            onClick={() => onComplete()}
            className={cn(
              "shrink-0 rounded-md px-2 py-1 font-mono text-[11px] uppercase tracking-[0.08em]",
              "text-muted-foreground transition-colors hover:text-foreground hover:bg-secondary/60",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
            )}
          >
            Show full page →
          </button>
        ) : null}
      </div>

      <div className="relative mt-4 min-h-[60svh]">
        <AnimatePresence mode="wait" custom={direction} initial={false}>
          <motion.div
            key={slot.id}
            custom={direction}
            variants={variants}
            initial="enter"
            animate="center"
            exit="exit"
            transition={reduceMotion ? { duration: 0.18 } : SPRING}
            className="absolute inset-0 flex flex-col"
          >
            {slot.render(api)}
          </motion.div>
        </AnimatePresence>
      </div>

      <p
        className="mt-3 text-center font-mono text-xs tabular-nums text-muted-foreground"
        aria-hidden
      >
        {index + 1} / {total}
      </p>
    </div>
  );
}

function ProgressBar({ value }: { value: number }) {
  return (
    <div
      className="h-1 w-full overflow-hidden rounded-full bg-muted"
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={1}
      aria-valuenow={value}
    >
      <motion.div
        className="h-full bg-accent"
        initial={false}
        animate={{ width: `${Math.round(value * 100)}%` }}
        transition={{ type: "spring", stiffness: 240, damping: 30 }}
      />
    </div>
  );
}

/**
 * Convenience wrapper for the standard card chrome — eyebrow + prompt +
 * body + back/next footer. Slot render functions can use this when the
 * card matches the default shape (most do); custom slots can drop it
 * and lay the body out themselves.
 */
export interface CardShellProps {
  api: CardSlotApi;
  eyebrow?: string;
  prompt: ReactNode;
  children: ReactNode;
  /** Override the next button label ("Next" / "Finish"). */
  nextLabel?: string;
  /** Hint shown beneath the footer (e.g. "⌘↵ to submit"). */
  footerHint?: ReactNode;
}

export function CardShell({
  api,
  eyebrow = "Reflect",
  prompt,
  children,
  nextLabel,
  footerHint,
}: CardShellProps) {
  return (
    <Card className="flex flex-1 flex-col bg-card shadow-md">
      <CardContent className="flex flex-1 flex-col gap-6 px-6 py-8 sm:px-10 sm:py-12">
        <header className="space-y-2">
          <p className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground">
            {eyebrow}
          </p>
          <h2 className="border-l-2 border-accent/60 pl-3 font-serif text-h2 leading-tight">
            {prompt}
          </h2>
        </header>

        <div className="flex flex-1 flex-col">{children}</div>

        <footer className="flex items-center justify-between gap-3 pt-2">
          <button
            type="button"
            onClick={api.back}
            disabled={api.index === 0 || api.busy}
            className={cn(
              "inline-flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-sm",
              "text-muted-foreground transition-colors hover:text-foreground hover:bg-secondary/60",
              "disabled:opacity-30 disabled:hover:bg-transparent disabled:hover:text-muted-foreground",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
            )}
            aria-label="Previous card"
          >
            <ChevronLeft className="h-4 w-4" aria-hidden />
            Back
          </button>
          <button
            type="button"
            onClick={api.advance}
            disabled={api.busy}
            className={cn(
              "inline-flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground",
              "transition-colors hover:bg-primary/90 disabled:opacity-60",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 ring-offset-background",
            )}
          >
            {nextLabel ?? (api.isLast ? "Finish" : "Next")}
          </button>
        </footer>

        {footerHint ? (
          <p className="font-mono text-[11px] text-muted-foreground/80" aria-hidden>
            {footerHint}
          </p>
        ) : null}
      </CardContent>
    </Card>
  );
}
