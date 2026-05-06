/// <reference lib="webworker" />
/// <reference types="vite-plugin-pwa/info" />
//
// Custom service worker (built via vite-plugin-pwa `injectManifest`).
//
// Phase 1 ships only the Workbox precache + an autoUpdate skipWaiting flow,
// so the SW exists and registers cleanly. Phase 5 adds:
//   - 'push' handler → showNotification with the reminder body
//   - 'notificationclick' → focus or open '/today'
//   - 'pushsubscriptionchange' → re-subscribe + POST to /api/push/subscriptions
//
// Keep this file minimal until Phase 5 — every line ships to every client.

import { precacheAndRoute, cleanupOutdatedCaches } from "workbox-precaching";

declare const self: ServiceWorkerGlobalScope & {
  __WB_MANIFEST: Array<string | { url: string; revision: string | null }>;
};

cleanupOutdatedCaches();
precacheAndRoute(self.__WB_MANIFEST);

self.addEventListener("install", () => {
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});
