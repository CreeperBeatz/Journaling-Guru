import { useQuery } from "@tanstack/react-query";

import { ApiError } from "@/api/client";
import { getReflectionByWeek, type ReflectionResponse } from "@/features/reflection/api";

// shiftDays returns the YYYY-MM-DD `days` away from `iso` (negative
// values go backward). Pure date math via UTC midnight to avoid DST
// drift — the canonical wire format never carries a time component.
export function shiftDays(iso: string, days: number): string {
  const [y, m, d] = iso.split("-").map(Number);
  const t = new Date(Date.UTC(y, m - 1, d));
  t.setUTCDate(t.getUTCDate() + days);
  const yy = t.getUTCFullYear();
  const mm = String(t.getUTCMonth() + 1).padStart(2, "0");
  const dd = String(t.getUTCDate()).padStart(2, "0");
  return `${yy}-${mm}-${dd}`;
}

// useReflectionByWeek — fetches the reflection state for a (computed)
// week_start derived from a localDate the user just navigated to. The
// query is enabled unconditionally; the consumer checks `started` to
// decide whether to show the Weekly tab.
export function useReflectionByWeek(localDate: string) {
  // The wizard treats localDate as the week_end (= reflection_weekday).
  // week_start = week_end - 6 days.
  const weekStart = shiftDays(localDate, -6);
  return useQuery<ReflectionResponse, ApiError>({
    queryKey: ["reflection", "by-week", weekStart],
    queryFn: () => getReflectionByWeek(weekStart),
    staleTime: 60_000,
    retry: 0,
  });
}
