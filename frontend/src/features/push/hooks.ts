import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useState } from "react";

import { ApiError } from "@/api/client";
import { toast } from "@/components/ui/sonner";

import {
  fetchPushState,
  fetchVAPIDPublicKey,
  PushState,
  subscribePush,
  testPush,
  TestPushResult,
  unsubscribePush,
  VAPIDKeyResponse,
} from "./api";
import { pushSupported, urlB64ToUint8Array } from "./utils";

export const PUSH_STATE_KEY = ["push", "state"] as const;
export const VAPID_KEY = ["push", "vapid-public-key"] as const;

// Server-tracked device list. Refetches on Settings mount so unplugging
// from another tab is reflected. Disabled when push isn't supported —
// no point spending a request the user can never act on.
export function usePushState() {
  return useQuery<PushState, ApiError>({
    queryKey: PUSH_STATE_KEY,
    queryFn: fetchPushState,
    staleTime: 30_000,
    enabled: pushSupported(),
  });
}

// VAPID key load is cached forever — keys don't rotate within a session
// and a fresh fetch on every subscribe attempt would just round-trip.
export function useVAPIDPublicKey() {
  return useQuery<VAPIDKeyResponse, ApiError>({
    queryKey: VAPID_KEY,
    queryFn: fetchVAPIDPublicKey,
    staleTime: Infinity,
    retry: false,
    enabled: pushSupported(),
  });
}

// useBrowserSubscription tracks the SW's notion of the current
// subscription. We don't trust the server alone — the user could have
// revoked permission in their browser settings without telling us, and
// PushManager.getSubscription() is the only authoritative source on
// whether the device is still active.
//
// The state has three values:
//   "loading"       — still asking the SW
//   "subscribed"    — SW reports a PushSubscription
//   "unsubscribed"  — SW reports null
export type BrowserSubState = "loading" | "subscribed" | "unsubscribed";

export function useBrowserSubscription() {
  const [state, setState] = useState<BrowserSubState>("loading");
  const [subscription, setSubscription] = useState<PushSubscription | null>(null);

  const refresh = useCallback(async () => {
    if (!pushSupported()) {
      setState("unsubscribed");
      return;
    }
    try {
      const reg = await navigator.serviceWorker.ready;
      const sub = await reg.pushManager.getSubscription();
      setSubscription(sub);
      setState(sub ? "subscribed" : "unsubscribed");
    } catch {
      // SW isn't ready yet or has crashed; treat as unsubscribed so the
      // UI offers a fresh subscribe.
      setSubscription(null);
      setState("unsubscribed");
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  return { state, subscription, refresh };
}

interface SubscribeArgs {
  publicKey: string;
}

// useSubscribePush walks the full grant flow:
//   1. Notification.requestPermission — must be the user's first
//      gesture, so this is called from a click handler.
//   2. SW.pushManager.subscribe({ userVisibleOnly: true,
//      applicationServerKey: <bytes> }).
//   3. POST /api/push/subscribe with the JSON form of the subscription.
//
// Failures are surfaced as ApiError-shaped toasts so the caller can
// decide whether to render a "blocked" hint.
export function useSubscribePush(onChange?: () => void) {
  const qc = useQueryClient();

  return useMutation<PushSubscription, Error, SubscribeArgs>({
    mutationFn: async ({ publicKey }) => {
      if (!pushSupported()) {
        throw new Error("Push notifications aren't supported in this browser.");
      }
      const perm = await Notification.requestPermission();
      if (perm !== "granted") {
        throw new Error(
          perm === "denied"
            ? "Notifications are blocked. Enable them in your browser's site settings."
            : "Permission was dismissed.",
        );
      }
      const reg = await navigator.serviceWorker.ready;
      // Reuse any existing subscription — re-subscribing with the same
      // VAPID key is a no-op browser-side, but we still POST so the
      // server registers the binding for the current account.
      let sub = await reg.pushManager.getSubscription();
      if (!sub) {
        sub = await reg.pushManager.subscribe({
          userVisibleOnly: true,
          applicationServerKey: urlB64ToUint8Array(publicKey),
        });
      }
      const json = sub.toJSON() as {
        endpoint: string;
        keys?: { p256dh?: string; auth?: string };
      };
      if (!json.endpoint || !json.keys?.p256dh || !json.keys?.auth) {
        throw new Error("Subscription is missing keys; please try again.");
      }
      await subscribePush({
        endpoint: json.endpoint,
        keys: { p256dh: json.keys.p256dh, auth: json.keys.auth },
      });
      return sub;
    },
    onSuccess: () => {
      toast.success("Reminders enabled");
      qc.invalidateQueries({ queryKey: PUSH_STATE_KEY });
      onChange?.();
    },
    onError: (err) => {
      toast.error("Couldn't enable reminders", { description: err.message });
    },
  });
}

interface UnsubscribeArgs {
  subscription: PushSubscription;
}

// useUnsubscribePush mirrors the subscribe flow in reverse: tell the
// browser, then tell the server. We tell the server first so a
// half-broken state (browser unsub'd but server still has the row)
// can't ghost-deliver one more reminder before housekeeping.
export function useUnsubscribePush(onChange?: () => void) {
  const qc = useQueryClient();
  return useMutation<void, Error, UnsubscribeArgs>({
    mutationFn: async ({ subscription }) => {
      const endpoint = subscription.endpoint;
      try {
        await unsubscribePush(endpoint);
      } catch {
        // Server may not have the row (e.g. it 410'd already). Continue
        // to the browser-side unsub anyway — the device shouldn't get
        // future reminders either way.
      }
      await subscription.unsubscribe();
    },
    onSuccess: () => {
      toast.success("Reminders disabled on this device");
      qc.invalidateQueries({ queryKey: PUSH_STATE_KEY });
      onChange?.();
    },
    onError: (err) => {
      toast.error("Couldn't disable reminders", { description: err.message });
    },
  });
}

// useTestPush fires the debug endpoint and surfaces the delivery tally.
// Useful when troubleshooting "I subscribed but nothing arrives" — the
// 200 vs 502 dichotomy tells the user whether the server reached the
// push service or not.
export function useTestPush() {
  return useMutation<TestPushResult, ApiError>({
    mutationFn: () => testPush(),
    onSuccess: (res) => {
      if (res.delivered > 0) {
        toast.success(`Test sent to ${res.delivered} device(s)`);
      } else {
        toast.message("No deliveries", {
          description: `Gone: ${res.gone}, retryable: ${res.retryable}.`,
        });
      }
    },
    onError: (err) => {
      toast.error("Test push failed", { description: err.message });
    },
  });
}
