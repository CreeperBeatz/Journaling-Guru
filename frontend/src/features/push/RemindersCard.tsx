import { Bell, BellOff, Info, Smartphone } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useMe } from "@/features/auth/useAuth";

import {
  useBrowserSubscription,
  usePushState,
  useSubscribePush,
  useTestPush,
  useUnsubscribePush,
  useVAPIDPublicKey,
} from "./hooks";
import { isIOS, isStandalone, labelForUserAgent, pushSupported } from "./utils";

// RemindersCard renders inside Settings → Notifications. Three states:
//   1. Browser doesn't support push → static "not supported" copy.
//   2. iOS Safari, not installed → "Add to Home Screen" instructions.
//   3. Supported → Subscribe / Unsubscribe + device list + test button.
export function RemindersCard() {
  const me = useMe();
  const supported = pushSupported();
  const onIOSWithoutInstall = isIOS() && !isStandalone();

  const vapid = useVAPIDPublicKey();
  const browserSub = useBrowserSubscription();
  const state = usePushState();
  const subscribe = useSubscribePush(() => browserSub.refresh());
  const unsubscribe = useUnsubscribePush(() => browserSub.refresh());
  const test = useTestPush();

  const reminderTime = me.data?.reminder_time?.slice(0, 5) ?? "—";

  if (!supported) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="font-serif">Daily push reminder</CardTitle>
          <CardDescription>
            Push notifications aren't supported in this browser. Try Chrome,
            Firefox, Edge, or Safari (iOS 16.4+ in Home Screen mode).
          </CardDescription>
        </CardHeader>
      </Card>
    );
  }

  if (onIOSWithoutInstall) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="font-serif">Add to Home Screen first</CardTitle>
          <CardDescription>
            On iOS, push notifications only work after you install Journaling Guru as
            an app. Tap the Share icon in Safari, then "Add to Home Screen,"
            and re-open from the Home Screen icon — the subscribe button will
            light up here.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex items-start gap-2 rounded-md border border-border/70 bg-muted/40 p-3 text-sm">
            <Smartphone className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
            <p className="text-muted-foreground">
              This is an Apple platform requirement, not a Journaling Guru choice — the
              same gate applies to every PWA.
            </p>
          </div>
        </CardContent>
      </Card>
    );
  }

  if (vapid.isError) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="font-serif">Daily push reminder</CardTitle>
          <CardDescription>
            Push isn't configured on the server yet. Run{" "}
            <code className="font-mono">make vapid</code> and add the keys to
            your <code className="font-mono">.env</code>.
          </CardDescription>
        </CardHeader>
      </Card>
    );
  }

  const subscribed = browserSub.state === "subscribed";
  const loadingState = browserSub.state === "loading" || vapid.isPending;
  const devices = state.data?.devices ?? [];

  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-serif">Daily push reminder</CardTitle>
        <CardDescription>
          A nudge at {reminderTime} (your local time) to come back and reflect.
          Subscribe per device — your phone and laptop are separate.
        </CardDescription>
      </CardHeader>

      <CardContent className="space-y-4">
        <div className="flex flex-wrap items-center gap-3">
          {subscribed ? (
            <Button
              variant="secondary"
              onClick={() =>
                browserSub.subscription &&
                unsubscribe.mutate({ subscription: browserSub.subscription })
              }
              disabled={unsubscribe.isPending || !browserSub.subscription}
            >
              <BellOff className="size-4" />
              {unsubscribe.isPending ? "Disabling…" : "Disable on this device"}
            </Button>
          ) : (
            <Button
              onClick={() => {
                if (!vapid.data?.public_key) return;
                subscribe.mutate({ publicKey: vapid.data.public_key });
              }}
              disabled={subscribe.isPending || loadingState || !vapid.data}
            >
              <Bell className="size-4" />
              {subscribe.isPending ? "Enabling…" : "Enable on this device"}
            </Button>
          )}

          {subscribed ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() => test.mutate()}
              disabled={test.isPending}
            >
              {test.isPending ? "Sending…" : "Send test notification"}
            </Button>
          ) : null}
        </div>

        {/* iOS reminder for installed PWA users — they bypass the
            "install first" branch above, but iOS still needs a tap on
            the prompt the OS shows during subscribe. */}
        {isIOS() && isStandalone() && !subscribed ? (
          <p className="flex items-start gap-2 text-xs text-muted-foreground">
            <Info className="mt-0.5 size-3.5 shrink-0" />
            iOS will ask once for permission. If you tap "Don't Allow," you'll
            need to revoke it from Settings → Notifications → Journaling Guru before
            this works again.
          </p>
        ) : null}

        {state.data && state.data.count > 0 ? (
          <div className="space-y-2 rounded-md border border-border/70 bg-muted/30 p-3">
            <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
              Subscribed devices
            </p>
            <ul className="space-y-1 text-sm">
              {devices.map((d) => (
                <li key={d.id} className="flex justify-between gap-3">
                  <span className="text-foreground">
                    {labelForUserAgent(d.user_agent ?? null)}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    last seen {new Date(d.last_used_at).toLocaleDateString()}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}
