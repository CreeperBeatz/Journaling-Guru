// Single-palette app (ember) — the old multi-palette switcher was removed.
// What remains is the theme-color sync for iOS PWA chrome.

// iOS PWA chrome reads <meta name="theme-color">. The palette declares its
// chrome color as a CSS var (--theme-color, "r g b"); we read the resolved
// value off <html> so it picks up the active light/dark variant for free.
export function syncThemeColorMeta() {
  if (typeof document === "undefined") return;
  const tag = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]');
  if (!tag) return;
  const raw = getComputedStyle(document.documentElement).getPropertyValue("--theme-color").trim();
  if (!raw) return;
  tag.setAttribute("content", `rgb(${raw})`);
}
