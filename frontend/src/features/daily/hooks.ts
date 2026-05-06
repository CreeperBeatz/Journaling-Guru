import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { ApiError } from "@/api/client";
import { toast } from "@/components/ui/sonner";

import {
  DailyInput,
  DailyInputResponse,
  DailyInputUpsertBody,
  getDailyInput,
  saveDailyInput,
  updateDailyInputByDate,
} from "./api";

// Cache key shape: ['daily', 'today'] for the always-current today
// view, ['daily', date] for HistoryView. Today never includes the
// resolved date in the key — the server picks it, and refetch drives
// re-resolution after a tz/day_start change.
export const dailyInputKey = (date?: string) =>
  date ? (["daily", date] as const) : (["daily", "today"] as const);

export function useDailyInput(date?: string) {
  return useQuery<DailyInputResponse, ApiError>({
    queryKey: dailyInputKey(date),
    queryFn: () => getDailyInput(date),
    staleTime: 30_000,
  });
}

interface SaveContext {
  prev?: DailyInputResponse;
}

// Today: PUT /api/daily/inputs. Optimistic: write straight into the
// cache, roll back on failure. Stats keys are invalidated so the
// SummariesPage chart updates next time the user navigates there.
export function useSaveDailyInput() {
  const qc = useQueryClient();
  return useMutation<
    DailyInput | { deleted: boolean; local_date: string },
    ApiError,
    DailyInputUpsertBody,
    SaveContext
  >({
    mutationFn: (body) => saveDailyInput(body),
    onMutate: async (body) => {
      await qc.cancelQueries({ queryKey: dailyInputKey() });
      const prev = qc.getQueryData<DailyInputResponse>(dailyInputKey());
      qc.setQueryData<DailyInputResponse>(dailyInputKey(), (old) => {
        if (!old) return old;
        const isEmpty =
          body.mood_score === null && body.emotions.length === 0 && body.notes === "";
        if (isEmpty) return { ...old, input: null };
        const baseId =
          (old.input?.id as string | undefined) ?? `temp-${old.local_date}`;
        return {
          local_date: old.local_date,
          input: {
            id: baseId,
            local_date: old.local_date,
            mood_score: body.mood_score,
            emotions: body.emotions,
            notes: body.notes,
            created_at: old.input?.created_at ?? new Date().toISOString(),
            updated_at: new Date().toISOString(),
          },
        };
      });
      return { prev };
    },
    onError: (err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(dailyInputKey(), ctx.prev);
      toast.error("Couldn't save", { description: err.message });
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: dailyInputKey() });
      // Stats are computed from daily_inputs server-side; bust them so
      // the next /summaries visit refetches fresh.
      qc.invalidateQueries({ queryKey: ["summaries", "stats"] });
    },
  });
}

// HistoryView: PATCH /api/daily/inputs/by-date/:date.
export function useUpdateDailyInput(localDate: string) {
  const qc = useQueryClient();
  return useMutation<
    DailyInput | { deleted: boolean; local_date: string },
    ApiError,
    DailyInputUpsertBody,
    SaveContext
  >({
    mutationFn: (body) => updateDailyInputByDate(localDate, body),
    onMutate: async (body) => {
      await qc.cancelQueries({ queryKey: dailyInputKey(localDate) });
      const prev = qc.getQueryData<DailyInputResponse>(dailyInputKey(localDate));
      qc.setQueryData<DailyInputResponse>(dailyInputKey(localDate), (old) => {
        if (!old) return old;
        const isEmpty =
          body.mood_score === null && body.emotions.length === 0 && body.notes === "";
        if (isEmpty) return { ...old, input: null };
        const baseId =
          (old.input?.id as string | undefined) ?? `temp-${old.local_date}`;
        return {
          local_date: old.local_date,
          input: {
            id: baseId,
            local_date: old.local_date,
            mood_score: body.mood_score,
            emotions: body.emotions,
            notes: body.notes,
            created_at: old.input?.created_at ?? new Date().toISOString(),
            updated_at: new Date().toISOString(),
          },
        };
      });
      return { prev };
    },
    onError: (err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(dailyInputKey(localDate), ctx.prev);
      toast.error("Couldn't save", { description: err.message });
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: dailyInputKey(localDate) });
      qc.invalidateQueries({ queryKey: ["summaries", "stats"] });
    },
  });
}
