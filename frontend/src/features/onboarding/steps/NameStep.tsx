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
  onSubmit: (displayName: string) => Promise<void>;
  onBack?: () => void;
}

export function NameStep({ draft, setDraft, onSubmit, onBack }: Props) {
  const [pending, setPending] = useState(false);

  const handleContinue = async () => {
    setPending(true);
    try {
      await onSubmit(draft.displayName.trim());
    } finally {
      setPending(false);
    }
  };

  return (
    <Card>
      <CardHeader className="space-y-2">
        <p className="text-xs uppercase tracking-wider text-muted-foreground">
          Your name
        </p>
        <CardTitle className="font-serif text-2xl">
          How should we call you?
        </CardTitle>
        <CardDescription>
          You can leave it blank if you're not comfortable sharing it.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-2 text-sm">
          <span className="text-foreground">We will call you</span>
          <Input
            id="onb-display-name"
            value={draft.displayName}
            onChange={(e) =>
              setDraft((d) => ({ ...d, displayName: e.target.value }))
            }
            placeholder="traveler"
            maxLength={200}
            autoComplete="given-name"
            autoFocus
            className="flex-1"
            aria-label="Your name"
          />
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
