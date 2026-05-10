import { useEffect, useState } from "react";

export const PALETTES = ["ember"] as const;
export type Palette = (typeof PALETTES)[number];

export const PALETTE_STORAGE_KEY = "journai.palette";
export const DEFAULT_PALETTE: Palette = "ember";

function isPalette(v: unknown): v is Palette {
  return typeof v === "string" && (PALETTES as readonly string[]).includes(v);
}

export function readStoredPalette(): Palette {
  if (typeof window === "undefined") return DEFAULT_PALETTE;
  try {
    const raw = window.localStorage.getItem(PALETTE_STORAGE_KEY);
    return isPalette(raw) ? raw : DEFAULT_PALETTE;
  } catch {
    return DEFAULT_PALETTE;
  }
}

function initialPalette(): Palette {
  if (typeof document === "undefined") return DEFAULT_PALETTE;
  const attr = document.documentElement.getAttribute("data-palette");
  return isPalette(attr) ? attr : readStoredPalette();
}

export function usePalette() {
  const [palette] = useState<Palette>(() => initialPalette());

  useEffect(() => {
    document.documentElement.setAttribute("data-palette", palette);
    try {
      window.localStorage.setItem(PALETTE_STORAGE_KEY, palette);
    } catch {
      /* private mode / quota exceeded — ignore */
    }
    syncThemeColorMeta();
  }, [palette]);

  return { palette };
}

// iOS PWA chrome reads <meta name="theme-color">. The active palette declares
// its chrome color as a CSS var (--theme-color, "r g b"); we read the resolved
// value off <html> so it picks up the active light/dark variant for free.
export function syncThemeColorMeta() {
  if (typeof document === "undefined") return;
  const tag = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]');
  if (!tag) return;
  const raw = getComputedStyle(document.documentElement).getPropertyValue("--theme-color").trim();
  if (!raw) return;
  tag.setAttribute("content", `rgb(${raw})`);
}
