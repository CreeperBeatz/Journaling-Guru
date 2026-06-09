import { useEffect } from "react";
import { useTheme } from "next-themes";

import { syncThemeColorMeta } from "@/lib/palette";

// Mounted once at the app root. Keeps the iOS PWA chrome (<meta name="theme-color">)
// aligned with the resolved light/dark mode (next-themes flipping the .dark
// class on <html>) so we don't need a MutationObserver to bridge the two.
export function PaletteSync() {
  const { resolvedTheme } = useTheme();
  useEffect(() => {
    syncThemeColorMeta();
  }, [resolvedTheme]);
  return null;
}
