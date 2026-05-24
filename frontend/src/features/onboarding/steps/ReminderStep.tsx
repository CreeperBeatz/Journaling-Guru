import { useState } from "react";
import { Bell, Smartphone } from "lucide-react";

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  useBrowserSubscription,
  useSubscribePush,
  useVAPIDPublicKey,
} from "@/features/push/hooks";
import { isIOS, isStandalone, pushSupported } from "@/features/push/utils";

import type { OnboardingDraft } from "../OnboardingFlow";

interface Props {
  draft: OnboardingDraft;
  setDraft: React.Dispatch<React.SetStateAction<OnboardingDraft>>;
  onSubmit: (next: {
    reminderTime: string;
    reminderEnabled: boolean;
  }) => Promise<void>;
  onBack?: () => void;
}

// ReminderStep saves reminder_time + reminder_enabled, then optionally
// kicks the browser subscribe flow inline. iOS-non-PWA users see the
// A2HS callout and continue without subscribing — they finish onboarding
// and can enable push later from Settings → Notifications once installed.
export function ReminderStep({ draft, setDraft, onSubmit, onBack }: Props) {
  const supported = pushSupported();
  const onIOSWithoutInstall = isIOS() && !isStandalone();

  const vapid = useVAPIDPublicKey();
  const browserSub = useBrowserSubscription();
  const subscribe = useSubscribePush(() => browserSub.refresh());

  const [pending, setPending] = useState(false);
  const subscribed = browserSub.state === "subscribed";

  const handleContinue = async () => {
    setPending(true);
    try {
      await onSubmit({
        reminderTime: draft.reminderTime,
        reminderEnabled: draft.reminderEnabled,
      });
      // Push subscribe is best-effort: if the user tapped "Enable push"
      // we fire it, but a denied permission won't block the flow.
      if (
        draft.reminderEnabled &&
        supported &&
        !onIOSWithoutInstall &&
        !subscribed &&
        vapid.data?.public_key
      ) {
        try {
          await subscribe.mutateAsync({ publicKey: vapid.data.public_key });
        } catch {
          /* toast surfaced by the hook */
        }
      }
    } finally {
      setPending(false);
    }
  };

  return (
    <Card>
      <CardHeader className="space-y-2">
        <p className="text-xs uppercase tracking-wider text-muted-foreground">
          Daily reminder
        </p>
        <CardTitle className="font-serif text-2xl">
          When does your day usually settle?
        </CardTitle>
        <CardDescription>
          People usually fill in their journal at the end of the day, once
          everything has happened. Pick the time that fits yours — we'll
          nudge you then.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-2 text-sm">
          <span className="text-foreground">I want to fill in my journal at</span>
          <Input
            id="onb-reminder-time"
            type="time"
            value={draft.reminderTime}
            onChange={(e) =>
              setDraft((d) => ({ ...d, reminderTime: e.target.value }))
            }
            className="w-auto"
            aria-label="Reminder time"
          />
          <span className="text-foreground">o'clock.</span>
        </div>

        {!supported ? (
          <p className="text-xs text-muted-foreground">
            Push notifications aren't supported in this browser. We'll still
            save the time — you can enable email reminders later.
          </p>
        ) : onIOSWithoutInstall ? (
          <div className="flex items-start gap-2 rounded-md border border-border/70 bg-muted/40 p-3 text-xs">
            <Smartphone className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
            <p className="text-muted-foreground leading-relaxed">
              On iOS, push notifications need the app installed. Tap the
              Share icon in Safari, then "Add to Home Screen," and re-open
              from the icon. You can finish setup now and enable push later
              from Settings.
            </p>
          </div>
        ) : (
          <button
            type="button"
            role="checkbox"
            aria-checked={draft.reminderEnabled}
            onClick={() =>
              setDraft((d) => ({ ...d, reminderEnabled: !d.reminderEnabled }))
            }
            className={cn(
              "flex w-full items-center justify-between gap-3 rounded-md border px-3 py-2.5 text-left transition-colors",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 ring-offset-background",
              draft.reminderEnabled
                ? "border-primary/40 bg-primary/5"
                : "border-border bg-background hover:bg-muted/40",
            )}
          >
            <span className="flex items-center gap-2 text-sm">
              <Bell className="size-4 text-muted-foreground" />
              Send me a push notification
              {subscribed ? (
                <span className="rounded-full bg-emerald-500/10 px-2 py-0.5 text-[10px] font-medium uppercase tracking-wider text-emerald-700 dark:text-emerald-400">
                  Active
                </span>
              ) : null}
            </span>
            <span
              aria-hidden
              className={cn(
                "relative h-5 w-9 rounded-full transition-colors",
                draft.reminderEnabled ? "bg-primary" : "bg-muted-foreground/30",
              )}
            >
              <span
                className={cn(
                  "absolute top-0.5 size-4 rounded-full bg-background shadow-xs transition-transform",
                  draft.reminderEnabled ? "translate-x-4" : "translate-x-0.5",
                )}
              />
            </span>
          </button>
        )}

        {supported && !onIOSWithoutInstall && draft.reminderEnabled && !subscribed ? (
          <p className="text-xs text-muted-foreground">
            We'll ask your browser for permission when you continue.
          </p>
        ) : null}

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
