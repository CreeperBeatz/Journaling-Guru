import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { ApiError } from "@/api/client";
import { toast } from "@/components/ui/sonner";

import {
  getStats,
  getSummary,
  getSummaryJobStatus,
  listSummaries,
  regenerateSummary,
  StatsResponse,
  Summary,
  SummaryJob,
  type PeriodType,
} from "./api";

export const SUMMARIES_KEY = (period: PeriodType) =>
  ["summaries", "list", period] as const;
export const SUMMARY_KEY = (id: string) => ["summaries", "detail", id] as const;
export const STATS_KEY = (days: number) => ["summaries", "stats", days] as const;
export const SUMMARY_JOB_KEY = (period_type: PeriodType, period_start: string) =>
  ["summaries", "job", period_type, period_start] as const;

export function useSummaries(period: PeriodType, limit?: number) {
  return useQuery<Summary[], ApiError>({
    queryKey: SUMMARIES_KEY(period),
    queryFn: async () => (await listSummaries(period, limit)).summaries,
    staleTime: 60_000,
  });
}

export function useSummary(id: string | undefined) {
  return useQuery<Summary, ApiError>({
    queryKey: SUMMARY_KEY(id ?? ""),
    queryFn: () => getSummary(id!),
    enabled: !!id,
    staleTime: 5 * 60_000,
  });
}

export function useStats(days = 90) {
  return useQuery<StatsResponse, ApiError>({
    queryKey: STATS_KEY(days),
    queryFn: () => getStats(days),
    staleTime: 60_000,
  });
}

// Regenerate is async on the backend (202). The list refetch picks up
// the new row when the worker finishes — which may be 30+ seconds for a
// yearly. The companion useSummaryJobStatus hook polls the queue row so
// SummaryDetail can render a live banner; here we just invalidate it on
// success so the next poll picks up the freshly-armed `pending` row.
export function useRegenerateSummary() {
  const qc = useQueryClient();
  return useMutation<
    { triggered: boolean; period_type: PeriodType; period_start: string },
    ApiError,
    { period_type: PeriodType; period_start: string }
  >({
    mutationFn: ({ period_type, period_start }) =>
      regenerateSummary(period_type, period_start),
    onSuccess: (res, vars) => {
      if (!res.triggered) {
        toast("Already in progress", {
          description: "A regeneration for this period is already running.",
        });
      }
      // Refetch the job-status row immediately so the banner shows
      // without waiting for the next poll tick.
      qc.invalidateQueries({
        queryKey: SUMMARY_JOB_KEY(vars.period_type, vars.period_start),
      });
    },
    onError: (err) => {
      toast.error("Couldn't regenerate", { description: err.message });
    },
  });
}

// useSummaryJobStatus polls the summary_jobs row for a (period_type,
// period_start) pair while the job is in flight. Returns null when no
// job has ever been scheduled for the period (HTTP 404). Stops polling
// once the job reaches a terminal state.
//
// The 3s cadence trades freshness for backend pressure — daily summaries
// usually finish in 5-30s, weekly/monthly take longer; even at the upper
// bound this is ~20 polls per regen.
export function useSummaryJobStatus(
  period_type: PeriodType | undefined,
  period_start: string | undefined,
) {
  return useQuery<SummaryJob | null, ApiError>({
    queryKey:
      period_type && period_start
        ? SUMMARY_JOB_KEY(period_type, period_start)
        : ["summaries", "job", "disabled"],
    queryFn: async () => {
      try {
        return await getSummaryJobStatus(period_type!, period_start!);
      } catch (err) {
        if (err instanceof ApiError && err.status === 404) return null;
        throw err;
      }
    },
    enabled: !!(period_type && period_start),
    refetchInterval: (query) => {
      const j = query.state.data;
      if (!j) return false;
      return j.status === "pending" || j.status === "claimed" ? 3000 : false;
    },
    staleTime: 0,
  });
}
