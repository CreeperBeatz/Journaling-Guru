import { api } from "@/api/client";

// VAPID public-key envelope. Server returns the same string the SW
// feeds into PushManager.subscribe({ applicationServerKey }) — we
// pass it through verbatim, no decoding here (utils.ts handles the
// base64url → Uint8Array conversion the browser API requires).
export interface VAPIDKeyResponse {
  public_key: string;
}

// SubscriptionDevice is the trimmed projection /api/push/state
// returns: never the raw endpoint URL (kept server-side; the FE
// already has its current SW subscription locally). Used to render
// "Subscribed on 2 devices · last seen 2h ago" in Settings.
export interface SubscriptionDevice {
  id: string;
  user_agent?: string | null;
  last_used_at: string;
}

export interface PushState {
  count: number;
  devices: SubscriptionDevice[];
}

// Request shape expected by /api/push/subscribe — mirrors the JSON the
// browser's PushSubscription.toJSON() emits, so we forward it directly.
export interface SubscribeBody {
  endpoint: string;
  keys: { p256dh: string; auth: string };
}

export function fetchVAPIDPublicKey(): Promise<VAPIDKeyResponse> {
  return api<VAPIDKeyResponse>("/api/push/vapid-public-key");
}

export function fetchPushState(): Promise<PushState> {
  return api<PushState>("/api/push/state");
}

export function subscribePush(body: SubscribeBody): Promise<unknown> {
  return api("/api/push/subscribe", { method: "POST", body });
}

export function unsubscribePush(endpoint: string): Promise<unknown> {
  return api("/api/push/subscribe", {
    method: "DELETE",
    body: { endpoint },
  });
}

// /api/push/test fires a notification immediately to every device the
// user is subscribed on. Returns a delivery tally; the FE surfaces it
// via toast so the user knows their setup actually works.
export interface TestPushResult {
  delivered: number;
  gone: number;
  retryable: number;
  error?: string;
}

export function testPush(): Promise<TestPushResult> {
  return api<TestPushResult>("/api/push/test", { method: "POST" });
}
