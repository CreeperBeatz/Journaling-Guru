import { Button } from "@/components/ui/button";

import { useRegenerateSummary } from "@/features/summaries/hooks";

import { ReflectionResponse } from "../api";
import { WeeklySynthesisCard } from "./WeeklySynthesisCard";

interface Props {
  data: ReflectionResponse;
  onContinue: () => void;
  saving: boolean;
  /** Override the continue button label — monthly weeks continue to the
   * monthly letter sheet instead of straight to the chat. */
  continueLabel?: string;
}

// LetterCard — first card of the weekly wizard. Shows the model's
// synthesis (letter + theme chips) on its own so the reader can sit
// with it before the rest of the reflection. Continue takes the user
// to /weekly/chat where the model responds to the letter together with
// the user.
export function LetterCard({ data, onContinue, saving, continueLabel }: Props) {
  const regen = useRegenerateSummary();
  const letterReady =
    data.letter.trim() !== "" ||
    data.charged.trim() !== "" ||
    data.drained.trim() !== "" ||
    data.grateful.trim() !== "" ||
    data.insights.trim() !== "";

  return (
    <div className="space-y-6">
      <WeeklySynthesisCard
        data={data}
        regenerating={regen.isPending}
        onRegenerate={() =>
          regen.mutate({
            period_type: "week",
            period_start: data.week_start,
          })
        }
      />

      <div className="flex justify-end">
        <Button onClick={onContinue} disabled={saving || !letterReady}>
          {saving ? "Opening chat…" : continueLabel ?? "Continue to reflection"}
        </Button>
      </div>
    </div>
  );
}
