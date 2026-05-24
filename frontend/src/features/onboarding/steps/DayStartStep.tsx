import { useState } from "react";

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";

import type { OnboardingDraft } from "../OnboardingFlow";

interface Props {
  draft: OnboardingDraft;
  setDraft: React.Dispatch<React.SetStateAction<OnboardingDraft>>;
  onSubmit: (dayStart: string) => Promise<void>;
  onBack?: () => void;
}

export function DayStartStep({ draft, setDraft, onSubmit, onBack }: Props) {
  const [pending, setPending] = useState(false);

  const handleContinue = async () => {
    setPending(true);
    try {
      await onSubmit(draft.dayStart);
    } finally {
      setPending(false);
    }
  };

  return (
    <Card>
      <CardHeader className="space-y-2">
        <p className="text-xs uppercase tracking-wider text-muted-foreground">
          Day reset
        </p>
        <CardTitle className="font-serif text-2xl">
          We won't punish you if you're a night bird
        </CardTitle>
        <CardDescription>
          So you can fill in your journal in bed without worry, even if you
          go to sleep after midnight.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="text-sm leading-relaxed">
          <p className="text-foreground">
            Anything before{" "}
            <Input
              id="onb-day-start"
              type="time"
              value={draft.dayStart}
              onChange={(e) =>
                setDraft((d) => ({ ...d, dayStart: e.target.value }))
              }
              className="inline-block h-8 w-auto align-middle"
              aria-label="Day reset time"
            />{" "}
            o'clock counts as the previous day.
          </p>
        </div>

        <div className="flex items-center justify-between gap-3">
          {onBack ? (
            <Button variant="ghost" onClick={onBack} disabled={pending}>
              Back
            </Button>
          ) : (
            <span />
          )}
          <Button onClick={handleContinue} disabled={pending}>
            {pending ? "Saving…" : "Continue"}
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
