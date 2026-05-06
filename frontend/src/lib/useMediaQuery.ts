import { useEffect, useState } from "react";

// Subscribes to a CSS media query and returns whether it currently matches.
// Defaults to false on first render to keep SSR-incompatible APIs out of
// the initial paint path; subsequent renders pick up the real value.
export function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(false);

  useEffect(() => {
    if (typeof window === "undefined") return;
    const mql = window.matchMedia(query);
    setMatches(mql.matches);
    const handler = (e: MediaQueryListEvent) => setMatches(e.matches);
    mql.addEventListener("change", handler);
    return () => mql.removeEventListener("change", handler);
  }, [query]);

  return matches;
}

// Pre-baked predicate for "is this a touch device" — used to gate swipe and
// pull-to-refresh behind real touch input rather than the viewport size.
export function useIsTouch(): boolean {
  return useMediaQuery("(hover: none) and (pointer: coarse)");
}
