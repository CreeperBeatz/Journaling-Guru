/// <reference lib="webworker" />
/// <reference types="vite-plugin-pwa/info" />
//
// Custom service worker (built via vite-plugin-pwa `injectManifest`).
//
// Current scope: precache + autoUpdate + runtime cache for safe-to-stale GETs.
// Phase 5 adds:
//   - 'push' handler → showNotification with the reminder body
//   - 'notificationclick' → focus or open '/today'
//   - 'pushsubscriptionchange' → re-subscribe + POST to /api/push/subscriptions
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
// Offline read support for these endpoints is Phase 5+ work; when we
// add it, we'll register POST/PATCH/DELETE matchers that clear the
// corresponding cache on success.
registerRoute(({ url }) => url.pathname.startsWith("/api/"), new NetworkOnly());

self.addEventListener("install", () => {
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});
