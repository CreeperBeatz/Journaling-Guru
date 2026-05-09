/// <reference lib="webworker" />
/// <reference types="vite-plugin-pwa/info" />
//
// Custom service worker (built via vite-plugin-pwa `injectManifest`).
//
// Scope:
//   - precache + autoUpdate + runtime cache for safe-to-stale GETs
//   - 'push'                   → showNotification with the reminder body
//   - 'notificationclick'      → focus an existing tab or open '/'
//   - 'pushsubscriptionchange' → re-subscribe + POST to /api/push/subscribe
//
// Keep this file minimal — every line ships to every client.

import { precacheAndRoute, cleanupOutdatedCaches } from "workbox-precaching";
import { registerRoute } from "workbox-routing";
import { NetworkOnly } from "workbox-strategies";

declare const self: ServiceWorkerGlobalScope & {
  __WB_MANIFEST: Array<string | { url: string; revision: string | null }>;
};

cleanupOutdatedCaches();
precacheAndRoute(self.__WB_MANIFEST);

// All /api/* routes are NetworkOnly. We write to /api/questions (POST,
// PATCH, DELETE, /reorder) and /api/entries (PUT, PATCH) from the same
// client, so any SW-side caching here needs explicit invalidation on
// write — otherwise the post-mutation refetch reads stale data and
// the UI snaps back to the pre-write state.
//
// TanStack Query's in-memory staleTime gives the perceived speed online.
// Offline read support for these endpoints is later work; when we add
// it, we'll register POST/PATCH/DELETE matchers that clear the
// corresponding cache on success.
registerRoute(({ url }) => url.pathname.startsWith("/api/"), new NetworkOnly());

self.addEventListener("install", () => {
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});

// ---------- Push notifications ----------
//
// Payload shape produced by backend/internal/jobs/push_worker.go:
//   { title, body, url?, tag? }
// Defensive defaults handle the no-payload case (some push services
// strip the body when the device is offline-buffered for too long).

interface PushPayload {
  title?: string;
  body?: string;
  url?: string;
  tag?: string;
}

function parsePush(event: PushEvent): PushPayload {
  if (!event.data) return {};
  try {
    return event.data.json() as PushPayload;
  } catch {
    return { body: event.data.text() };
  }
}

self.addEventListener("push", (event) => {
  const payload = parsePush(event);
  const title = payload.title ?? "Journaling Guru";
  const body = payload.body ?? "Time to reflect.";
  const tag = payload.tag ?? "reminder";

  event.waitUntil(
    self.registration.showNotification(title, {
      body,
      tag,
      // Badging assets — falls back to the OS default if these 404 in
      // dev (we ship pwa-192.png from /public).
      icon: "/pwa-192.png",
      badge: "/pwa-192.png",
      data: { url: payload.url ?? "/" },
    }),
  );
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const targetUrl =
    (event.notification.data as { url?: string } | undefined)?.url ?? "/";

  event.waitUntil(
    (async () => {
      // Prefer focusing an existing window — tap-to-launch shouldn't
      // double-open the app if the user already has it open in a tab.
      const all = await self.clients.matchAll({
        type: "window",
        includeUncontrolled: true,
      });
      for (const client of all) {
        const url = new URL(client.url);
        if (url.origin === self.location.origin && "focus" in client) {
          await (client as WindowClient).focus();
          if ("navigate" in client) {
            try {
              await (client as WindowClient).navigate(targetUrl);
            } catch {
              // Cross-document navigate fails on some Safari builds;
              // the focus alone is acceptable.
            }
          }
          return;
        }
      }
      await self.clients.openWindow(targetUrl);
    })(),
  );
});

// pushsubscriptionchange is the iOS-reliability linchpin. Triggers when
// the push service rotates the endpoint (e.g. after an iOS reinstall,
// or APNs token rotation). The browser hands us no subscription; we
// re-subscribe with the same VAPID key and POST the new one to the
// server — without this, iOS users would silently lose reminders on
// every reboot.
//
// We pull the VAPID key from the public endpoint (no auth required)
// every time, since a SW has no access to React state.
self.addEventListener("pushsubscriptionchange", (event) => {
  event.waitUntil(
    (async () => {
      try {
        const keyRes = await fetch("/api/push/vapid-public-key", {
          credentials: "include",
        });
        if (!keyRes.ok) return;
        const { public_key } = (await keyRes.json()) as { public_key: string };
        const applicationServerKey = b64UrlToUint8Array(public_key);

        const sub = await self.registration.pushManager.subscribe({
          userVisibleOnly: true,
          applicationServerKey,
        });
        const json = sub.toJSON() as {
          endpoint: string;
          keys?: { p256dh?: string; auth?: string };
        };
        if (!json.endpoint || !json.keys?.p256dh || !json.keys?.auth) return;

        await fetch("/api/push/subscribe", {
          method: "POST",
          credentials: "include",
          headers: {
            "Content-Type": "application/json",
            "X-Requested-With": "fetch",
          },
          body: JSON.stringify({
            endpoint: json.endpoint,
            keys: { p256dh: json.keys.p256dh, auth: json.keys.auth },
          }),
        });
      } catch {
        // Best-effort — the user can re-subscribe from Settings if
        // this fails (e.g. server briefly down during the rotation).
      }
    })(),
  );
});

function b64UrlToUint8Array(b64: string): Uint8Array<ArrayBuffer> {
  const padding = "=".repeat((4 - (b64.length % 4)) % 4);
  const normalized = (b64 + padding).replace(/-/g, "+").replace(/_/g, "/");
  const raw = atob(normalized);
  const buf = new ArrayBuffer(raw.length);
  const out = new Uint8Array(buf);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}
