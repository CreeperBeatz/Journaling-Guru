// Browser → Web Push helpers. Kept tiny and free of TanStack types so
// the SW can import the same functions if we ever need to (today the
// SW only re-subscribes via pushsubscriptionchange, which inlines its
// own conversion).

// urlB64ToUint8Array decodes the VAPID public key (base64url) into the
// raw bytes PushManager.subscribe({ applicationServerKey }) requires.
// Browsers don't accept the string form — they want a BufferSource.
//
// We allocate via `new ArrayBuffer(len)` (rather than the implicit
// constructor of Uint8Array) so the TS type is fixed to
// Uint8Array<ArrayBuffer> — required because PushManager.subscribe
// rejects Uint8Array<ArrayBufferLike> in modern lib.dom.d.ts.
export function urlB64ToUint8Array(b64: string): Uint8Array<ArrayBuffer> {
  const padding = "=".repeat((4 - (b64.length % 4)) % 4);
  const normalized = (b64 + padding).replace(/-/g, "+").replace(/_/g, "/");
  const raw = atob(normalized);
  const buf = new ArrayBuffer(raw.length);
  const out = new Uint8Array(buf);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

// arrayBufferToBase64Url is the inverse of urlB64ToUint8Array, used to
// serialize the subscription's keys (p256dh, auth) into the
// /api/push/subscribe payload. PushSubscription.toJSON() already does
// this for us — included here in case we ever build the body manually.
export function arrayBufferToBase64Url(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf);
  let bin = "";
  for (let i = 0; i < bytes.byteLength; i++) bin += String.fromCharCode(bytes[i]);
  return btoa(bin)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/g, "");
}

// pushSupported gates the entire RemindersCard. Fires false on:
//   - Node-side SSR (no window).
//   - Browsers without Notification API (deep-legacy IE, etc).
//   - Browsers without PushManager (older Safari).
//   - Browsers without service-worker support.
export function pushSupported(): boolean {
  if (typeof window === "undefined") return false;
  return (
    "serviceWorker" in navigator &&
    "PushManager" in window &&
    "Notification" in window
  );
}

// isIOS detects iOS / iPadOS Safari for the install-banner copy. The UA
// trick handles iPadOS where Safari now reports as macOS — we add the
// touch-points check to disambiguate.
export function isIOS(): boolean {
  if (typeof navigator === "undefined") return false;
  const ua = navigator.userAgent;
  if (/iPhone|iPod/.test(ua)) return true;
  // iPadOS 13+ identifies as Mac; only real iPads expose multi-touch
  // points >= 1 with a "Mac" UA.
  if (/Macintosh/.test(ua) && navigator.maxTouchPoints > 1) return true;
  return false;
}

// isStandalone reports whether the page is running as an installed PWA.
// On iOS, push only works in standalone mode — so we surface "Add to
// Home Screen" copy until the user installs.
export function isStandalone(): boolean {
  if (typeof window === "undefined") return false;
  if (window.matchMedia?.("(display-mode: standalone)").matches) return true;
  // iOS-specific (older spec): navigator.standalone
  const navAny = navigator as Navigator & { standalone?: boolean };
  return navAny.standalone === true;
}

// labelForUserAgent shortens a User-Agent string to the most useful
// piece (browser + OS) for the device list. Pure best-effort string
// matching — UA strings are notoriously messy, so we err on the side
// of a reasonable fallback.
export function labelForUserAgent(ua: string | null | undefined): string {
  if (!ua) return "Unknown device";
  const browser = ua.includes("Edg/")
    ? "Edge"
    : ua.includes("Firefox/")
    ? "Firefox"
    : ua.includes("Chrome/")
    ? "Chrome"
    : ua.includes("Safari/")
    ? "Safari"
    : "Browser";
  const os = ua.includes("iPhone")
    ? "iPhone"
    : ua.includes("iPad")
    ? "iPad"
    : ua.includes("Android")
    ? "Android"
    : ua.includes("Macintosh")
    ? "macOS"
    : ua.includes("Windows")
    ? "Windows"
    : ua.includes("Linux")
    ? "Linux"
    : "device";
  return `${browser} on ${os}`;
}
