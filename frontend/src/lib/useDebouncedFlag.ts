import { useEffect, useState } from "react";

// Returns true once `flag` has been continuously true for `delayMs`. Resets
// to false the moment `flag` flips back. Use to suppress sub-300ms loading
// indicators that flicker on healthy networks but do want to surface when
// a save is genuinely slow.
export function useDebouncedFlag(flag: boolean, delayMs: number): boolean {
  const [delayed, setDelayed] = useState(false);
  useEffect(() => {
    if (!flag) {
      setDelayed(false);
      return;
    }
    const t = setTimeout(() => setDelayed(true), delayMs);
    return () => clearTimeout(t);
  }, [flag, delayMs]);
  return delayed;
}
