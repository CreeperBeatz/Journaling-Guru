import { useQuery } from "@tanstack/react-query";

import { ApiError } from "@/api/client";
import { getReflectionByWeek, type ReflectionResponse } from "@/features/reflection/api";
import { DonePage } from "@/features/reflection/cards/DonePage";

// HistoryWeeklyReflection — read-only view of a past weekly reflection.
// Fed by GET /api/reflection/by-week/:week_start. Reuses the wizard's
// DonePage so the styling matches the live "you wrapped this week"
// view exactly.
export function HistoryWeeklyReflection({ weekStart }: { weekStart: string }) {
  const q = useQuery<ReflectionResponse, ApiError>({
    queryKey: ["reflection", "by-week", weekStart],
    queryFn: () => getReflectionByWeek(weekStart),
    staleTime: 60_000,
  });

  if (q.isPending) {
    return <p className="text-sm text-muted-foreground">Loading reflection…</p>;
  }
  if (q.isError) {
    return <p className="text-sm text-destructive">{q.error.message}</p>;
  }
  return <DonePage data={q.data!} completedAt={q.data!.completed_at} />;
}
