import { useCallback, useEffect, useState } from "react";

export const PALETTES = ["paper", "ember", "forest", "ocean", "slate"] as const;
export type Palette = (typeof PALETTES)[number];

export const PALETTE_LABEL: Record<Palette, string> = {
  paper: "Paper",
  ember: "Ember",
  forest: "Forest",
  ocean: "Ocean",
  slate: "Slate",
};

export const PALETTE_DESCRIPTION: Record<Palette, string> = {
  paper: "Warm cream + ink violet + terracotta — the canonical journal aesthetic.",
  ember: "Peach cream + burnt orange + deep teal — embers in candlelight.",
  forest: "Honey sand + deep moss + cranberry — leather notebook in a warm study.",
  ocean: "Warm sand + deep teal + sun gold — beach light, calm sea.",
  slate: "Warm clay + ink violet + magenta — saturated paper, modern.",
};

// Visual swatches for the picker. Each tuple is [bg, primary, accent] as
// CSS hsl() strings, mirroring the *light* mode of each palette so the chip
// reads correctly against the surrounding card. Kept in JS (not derived from
// CSS vars) because we want each chip to render its own palette regardless
// of which palette is active.
export const PALETTE_SWATCH: Record<Palette, [string, string, string]> = {
  paper: ["hsl(39 38% 96%)", "hsl(252 70% 50%)", "hsl(18 70% 52%)"],
  ember: ["hsl(24 44% 95%)", "hsl(22 80% 44%)", "hsl(190 60% 36%)"],
  forest: ["hsl(50 26% 94%)", "hsl(152 55% 30%)", "hsl(352 60% 46%)"],
  ocean: ["hsl(32 32% 94%)", "hsl(195 75% 32%)", "hsl(42 90% 46%)"],
  slate: ["hsl(36 14% 93%)", "hsl(252 70% 50%)", "hsl(330 72% 52%)"],
};

export const PALETTE_STORAGE_KEY = "journai.palette";
export const DEFAULT_PALETTE: Palette = "paper";

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

// Read what the anti-flash script already applied (data-palette on <html>),
// falling back to localStorage and then the default. Keeps initial state in
// sync with whatever paint the user already saw.
function initialPalette(): Palette {
  if (typeof document === "undefined") return DEFAULT_PALETTE;
  const attr = document.documentElement.getAttribute("data-palette");
  return isPalette(attr) ? attr : readStoredPalette();
}

export function usePalette() {
  const [palette, setPaletteState] = useState<Palette>(() => initialPalette());

  // Apply attribute + persist on every change. Effect runs after mount, so the
  // anti-flash path (in index.html) is what guarantees first paint matches.
  useEffect(() => {
    document.documentElement.setAttribute("data-palette", palette);
    try {
      window.localStorage.setItem(PALETTE_STORAGE_KEY, palette);
    } catch {
      /* private mode / quota exceeded — ignore */
    }
    syncThemeColorMeta();
  }, [palette]);

  const setPalette = useCallback((next: Palette) => setPaletteState(next), []);

  return { palette, setPalette, palettes: PALETTES };
}

// iOS PWA chrome reads <meta name="theme-color">. Each palette declares its
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
