import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { ApiError } from "@/api/client";
import { toast } from "@/components/ui/sonner";

import {
  getStats,
  getSummary,
  listSummaries,
  regenerateSummary,
  StatsResponse,
  Summary,
  type PeriodType,
} from "./api";

export const SUMMARIES_KEY = (period: PeriodType) =>
  ["summaries", "list", period] as const;
export const SUMMARY_KEY = (id: string) => ["summaries", "detail", id] as const;
export const STATS_KEY = (days: number) => ["summaries", "stats", days] as const;

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
// yearly. We optimistically toast and let TanStack Query's polling /
// invalidate cycle catch the update.
export function useRegenerateSummary() {
  const qc = useQueryClient();
  return useMutation<
    { triggered: boolean; period_type: PeriodType; period_start: string },
    ApiError,
    { period_type: PeriodType; period_start: string }
  >({
    mutationFn: ({ period_type, period_start }) =>
      regenerateSummary(period_type, period_start),
    onSuccess: (res) => {
      if (res.triggered) {
        toast("Regeneration queued", {
          description: "This can take up to a minute. Refresh to see the new draft.",
        });
      } else {
        toast("Already in progress", {
          description: "A regeneration for this period is already running.",
        });
      }
      // Invalidate so a poll/manual-refresh sees the eventual new row.
      qc.invalidateQueries({ queryKey: ["summaries"] });
    },
    onError: (err) => {
      toast.error("Couldn't regenerate", { description: err.message });
    },
  });
}
