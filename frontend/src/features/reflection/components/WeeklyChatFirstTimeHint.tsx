import { useEffect, useState } from "react";
import { X } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";

const STORAGE_KEY = "journai.weeklyChatHintDismissed";

// WeeklyChatFirstTimeHint shows once, explaining the reflection chat.
// Different localStorage key from ChatFirstTimeHint so dismissing the
// daily hint doesn't suppress this one.
export function WeeklyChatFirstTimeHint() {
  const [visible, setVisible] = useState(false);
  useEffect(() => {
    try {
      if (!localStorage.getItem(STORAGE_KEY)) {
        setVisible(true);
      }
    } catch {
      /* SSR / private mode — ignore */
    }
  }, []);

  if (!visible) return null;

  const dismiss = () => {
    try {
      localStorage.setItem(STORAGE_KEY, "1");
    } catch {
      /* ignore */
    }
    setVisible(false);
  };

  return (
    <Card className="mb-4 border-primary/30 bg-primary/5">
      <CardContent className="flex items-start gap-3 px-4 py-3 text-sm">
        <div className="flex-1">
          <p className="font-medium">A space to sit with your week.</p>
          <p className="mt-1 text-muted-foreground">
            We'll talk the letter through together — no rush. If something
            shapes up that feels worth committing to, I'll propose a tiny
            goal for the week ahead. You're never locked into anything.
          </p>
        </div>
        <Button
          type="button"
          size="icon"
          variant="ghost"
          onClick={dismiss}
          className="h-7 w-7 shrink-0"
          aria-label="Dismiss"
        >
          <X className="h-4 w-4" />
        </Button>
      </CardContent>
    </Card>
  );
}
