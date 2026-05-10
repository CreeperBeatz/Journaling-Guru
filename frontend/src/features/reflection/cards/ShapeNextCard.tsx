import { useState } from "react";
import { Sparkles } from "lucide-react";

import { Button } from "@/components/ui/button";

import { SmartShaperModal } from "@/features/goals/SmartShaperModal";

import { usePatchReflection } from "../hooks";

interface Props {
  onDone: () => void;
}

// Card 3 — shape what's next. Two CTAs: open the same SmartShaperModal
// the /goals page uses, or skip the wizard step entirely. Whichever
// path the user takes, the wizard advances to its done page after.
//
// On commit, we record the new goal id on the reflection row before
// finishing so DonePage can render it under "New goals" instead of
// bucketing by start_date alone.
export function ShapeNextCard({ onDone }: Props) {
  const [shaperOpen, setShaperOpen] = useState(false);
  const patch = usePatchReflection();

  const handleCreated = async ({ id }: { id: string }) => {
    try {
      await patch.mutateAsync({ new_goal_id: id });
    } finally {
      onDone();
    }
  };

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <h2 className="font-serif text-h2">Shape what's next</h2>
        <p className="text-sm text-muted-foreground">
          Want to commit to one small change for the coming week? You can
          shape a new goal — or skip if you'd rather not commit to
          anything new.
        </p>
      </header>

      <div className="flex flex-wrap gap-3">
        <Button onClick={() => setShaperOpen(true)} className="gap-1.5">
          <Sparkles className="h-4 w-4" />
          Set up new goals
        </Button>
        <Button variant="ghost" onClick={onDone}>
          Skip — no new goal this week
        </Button>
      </div>

      <SmartShaperModal
        open={shaperOpen}
        onOpenChange={setShaperOpen}
        onCreated={handleCreated}
      />
    </div>
  );
}
