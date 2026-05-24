import { useQuery } from "@tanstack/react-query";

import { ApiError } from "@/api/client";
import {
  getReflectionByWeek,
  type ReflectionResponse,
} from "@/features/reflection/api";
import { HistoryWeeklyChatTranscript } from "@/features/reflection/HistoryWeeklyChatTranscript";
import { WeeklySummary } from "@/features/reflection/WeeklySummary";
import { usePatchReflectionByWeek } from "@/features/reflection/hooks";

// HistoryWeeklyReflection — read-only view of a past weekly reflection.
// Reuses WeeklySummary (so the letter + editable surprise_text + per-
// goal notes look identical to the live page) but points the patch
// handler at PATCH /api/reflection/by-week/{week_start} so edits land
// on the right row. Replay is hidden — past weeks aren't replayable.
//
// The historical chat transcript renders as a collapsible card below,
// mirroring the daily HistoryChatTranscript pattern.
export function HistoryWeeklyReflection({ weekStart }: { weekStart: string }) {
  const q = useQuery<ReflectionResponse, ApiError>({
    queryKey: ["reflection", "by-week", weekStart],
    queryFn: () => getReflectionByWeek(weekStart),
    staleTime: 60_000,
  });
  const patch = usePatchReflectionByWeek(weekStart);

  if (q.isPending) {
    return <p className="text-sm text-muted-foreground">Loading reflection…</p>;
  }
  if (q.isError) {
    return <p className="text-sm text-destructive">{q.error.message}</p>;
  }
  return (
    <div className="space-y-6">
      <WeeklySummary
        data={q.data!}
        onPatch={(body) => patch.mutate(body)}
        patchPending={patch.isPending}
        showReplay={false}
        showRegenerate={false}
        showHeader={false}
      />
      <HistoryWeeklyChatTranscript weekStart={weekStart} />
    </div>
  );
}
