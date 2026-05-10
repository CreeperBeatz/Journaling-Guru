import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

import { SmartShaperInline } from "./SmartShaperInline";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // Called after a goal is successfully created. The id is forwarded so
  // callers (e.g. the weekly wizard) can record it on their own state
  // before reacting to the close.
  onCreated?: (goal: { id: string }) => void;
  // When provided, the in-modal "skip" button renders with the goals-
  // context label ("Skip the shaper, fill the form manually") and fires
  // this callback. Omit it when the parent owns its own skip (the
  // weekly wizard's "Skip — no new goal this week" button), so we
  // don't surface two competing skips.
  onFallback?: () => void;
}

// SmartShaperModal — tall flex dialog around SmartShaperInline. Anchors
// to the viewport bottom so the sticky-bottom composer inside the
// inline sits at the actual page bottom (matching /today's chat).
//
// Re-mount on open: the `key` flips on each open transition so the
// inline's local state (transcript, partial, pendingTool) resets each
// time the user opens the modal — matching the previous behaviour
// where closing always reset everything.
export function SmartShaperModal({ open, onOpenChange, onCreated, onFallback }: Props) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className={
          // Anchor to the viewport bottom (not centered) so the sticky
          // composer pill inside SmartShaperInline sits at the actual
          // page bottom.
          "fixed bottom-0 left-1/2 top-auto h-[90vh] max-w-2xl translate-y-0 -translate-x-1/2 " +
          "flex flex-col gap-0 p-0"
        }
      >
        <DialogHeader className="border-b border-border/60 px-6 pt-5 pb-3">
          <DialogTitle className="font-serif text-lg">Shape a new goal</DialogTitle>
        </DialogHeader>
        <div className="min-h-0 flex-1">
          {open ? (
            <SmartShaperInline
              key={open ? "open" : "closed"}
              onCreated={(goal) => {
                onCreated?.(goal);
                onOpenChange(false);
              }}
              onSkip={
                onFallback
                  ? () => {
                      onOpenChange(false);
                      onFallback();
                    }
                  : undefined
              }
              skipLabel="Skip the shaper, fill the form manually"
            />
          ) : null}
        </div>
      </DialogContent>
    </Dialog>
  );
}
