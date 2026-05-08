import { useEffect, useMemo, useRef, useState } from "react";
import { ChevronLeft, Loader2 } from "lucide-react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";

import { Card, CardContent } from "@/components/ui/card";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

export interface CardStackItem {
  id: string;
  prompt: string;
  initialBody?: string;
}

export interface CardStackProps {
  items: CardStackItem[];
  onSubmit: (item: CardStackItem, body: string) => void | Promise<void>;
  onComplete?: () => void;
  /**
   * Index to start on. When omitted, starts at the first item with no
   * `initialBody`; falls back to 0 if all items are pre-filled.
   */
  startIndex?: number;
  className?: string;
}

const SPRING = { type: "spring", stiffness: 380, damping: 30 } as const;

function firstEmptyIndex(items: CardStackItem[]): number {
  const idx = items.findIndex((it) => !(it.initialBody ?? "").trim());
  return idx === -1 ? 0 : idx;
}

/**
 * One question per card, full focus. Advance on submit (Enter / Cmd+Enter).
 * Swipe-back / ChevronLeft to revisit. The CardStack only renders one card
 * at a time — the parent decides when to swap the surface for `<PaperPage>`
 * (typically when all items have a body).
 */
export function CardStack({
  items,
  onSubmit,
  onComplete,
  startIndex,
  className,
}: CardStackProps) {
  const total = items.length;
  const initial = useMemo(
    () => (typeof startIndex === "number" ? startIndex : firstEmptyIndex(items)),
    // re-compute only when the count changes — switching the resolved index
    // mid-flow would yank a user off their current card.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [total],
  );
  const [index, setIndex] = useState(initial);
  const [direction, setDirection] = useState<1 | -1>(1);
  const [busy, setBusy] = useState(false);
  const reduceMotion = useReducedMotion();

  // Clamp if items shrink under us (e.g. question archived mid-flow).
  useEffect(() => {
    if (index >= total && total > 0) setIndex(total - 1);
  }, [index, total]);

  if (total === 0) return null;
  const item = items[index];
  if (!item) return null;

  const isLast = index === total - 1;

  const advance = async (body: string) => {
    if (busy) return;
    setBusy(true);
    try {
      await onSubmit(item, body);
    } finally {
      setBusy(false);
    }
    if (isLast) {
      onComplete?.();
      return;
    }
    setDirection(1);
    setIndex((i) => Math.min(i + 1, total - 1));
  };

  const goBack = () => {
    if (index === 0 || busy) return;
    setDirection(-1);
    setIndex((i) => i - 1);
  };

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
      <ProgressBar value={(index + 1) / total} />

      <div className="relative mt-4 min-h-[60svh]">
        <AnimatePresence mode="wait" custom={direction} initial={false}>
          <motion.div
            key={item.id}
            custom={direction}
            variants={variants}
            initial="enter"
            animate="center"
            exit="exit"
            transition={reduceMotion ? { duration: 0.18 } : SPRING}
            className="absolute inset-0 flex flex-col"
          >
            <CardStackBody
              item={item}
              busy={busy}
              isLast={isLast}
              canGoBack={index > 0}
              onSubmit={advance}
              onBack={goBack}
            />
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

interface CardStackBodyProps {
  item: CardStackItem;
  busy: boolean;
  isLast: boolean;
  canGoBack: boolean;
  onSubmit: (body: string) => void;
  onBack: () => void;
}

function CardStackBody({
  item,
  busy,
  isLast,
  canGoBack,
  onSubmit,
  onBack,
}: CardStackBodyProps) {
  const [draft, setDraft] = useState(item.initialBody ?? "");
  const ref = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    // Autofocus the textarea on mount so keyboard users can start typing
    // immediately. iOS Safari respects this only after a user gesture
    // tripped the page — that's already true for any non-initial card,
    // and acceptable for the first card.
    ref.current?.focus({ preventScroll: true });
  }, [item.id]);

  const handleSubmit = () => onSubmit(draft);
  const handleKey = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // Cmd/Ctrl+Enter advances. Plain Enter inserts a newline (textareas
    // are sentence/paragraph kind by default; short-answer kinds in the
    // future will swap to <Input> with plain Enter to advance).
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      handleSubmit();
    }
  };

  return (
    <Card className="flex flex-1 flex-col bg-card shadow-md">
      <CardContent className="flex flex-1 flex-col gap-6 px-6 py-8 sm:px-10 sm:py-12">
        <header className="space-y-2">
          <p className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground">
            Reflect
          </p>
          <h2 className="border-l-2 border-accent/60 pl-3 font-serif text-h2 leading-tight">
            {item.prompt}
          </h2>
        </header>

        <Textarea
          ref={ref}
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={handleKey}
          placeholder="Write whatever comes to mind…"
          rows={6}
          className={cn(
            "flex-1 resize-none border-transparent bg-transparent px-0 leading-prose text-body",
            "focus-visible:ring-0 focus-visible:ring-offset-0",
            "focus-visible:border-b-border focus-visible:border-b rounded-none",
          )}
        />

        <footer className="flex items-center justify-between gap-3 pt-2">
          <button
            type="button"
            onClick={onBack}
            disabled={!canGoBack || busy}
            className={cn(
              "inline-flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-sm",
              "text-muted-foreground transition-colors hover:text-foreground hover:bg-secondary/60",
              "disabled:opacity-30 disabled:hover:bg-transparent disabled:hover:text-muted-foreground",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
            )}
            aria-label="Previous question"
          >
            <ChevronLeft className="h-4 w-4" aria-hidden />
            Back
          </button>
          <button
            type="button"
            onClick={handleSubmit}
            disabled={busy}
            className={cn(
              "inline-flex items-center gap-2 rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground",
              "transition-colors hover:bg-primary/90 disabled:opacity-60",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 ring-offset-background",
            )}
          >
            {busy ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden />
            ) : null}
            {isLast ? "Finish" : "Next"}
          </button>
        </footer>

        <p className="font-mono text-[11px] text-muted-foreground/80" aria-hidden>
          ⌘↵ to submit · empty answers are allowed
        </p>
      </CardContent>
    </Card>
  );
}
