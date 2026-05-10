import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { ApiError } from "@/api/client";
import { toast } from "@/components/ui/sonner";
import { heatmapKey } from "@/features/journal/hooks";

import {
  DailyInputResponse,
  DailyInputUpsertBody,
  TagDayLink,
  Tag,
  getDailyInput,
  listTags,
  saveDailyInput,
  updateDailyInputByDate,
} from "./api";

// Cache key shape: ['daily', 'today'] for the always-current today
// view, ['daily', date] for HistoryView. Today never includes the
// resolved date in the key — the server picks it, and refetch drives
// re-resolution after a tz/day_start change.
export const dailyInputKey = (date?: string) =>
  date ? (["daily", date] as const) : (["daily", "today"] as const);

export const tagsKey = (valence?: Tag["valence"]) =>
  valence ? (["tags", valence] as const) : (["tags"] as const);

export function useDailyInput(date?: string) {
  return useQuery<DailyInputResponse, ApiError>({
    queryKey: dailyInputKey(date),
    queryFn: () => getDailyInput(date),
    staleTime: 30_000,
  });
}

export function useTags(valence?: Tag["valence"]) {
  return useQuery({
    queryKey: tagsKey(valence),
    queryFn: () => listTags(valence),
    staleTime: 60_000,
  });
}

interface SaveContext {
  prev?: DailyInputResponse;
}

// Optimistic helper: build the next DailyInputResponse from the
// outgoing body + the previous server snapshot. Tag IDs are looked up
// via the cached tag list so the optimistic pills render with the right
// labels (server returns labels too on the response — this is just for
// the in-flight ms).
function buildOptimistic(
  body: DailyInputUpsertBody,
  prev: DailyInputResponse | undefined,
  tagsLookup: Map<string, Tag>,
): DailyInputResponse | undefined {
  if (!prev) return prev;
  const isEmpty =
    body.mood === null &&
    body.drained_text === "" &&
    body.charged_text === "" &&
    body.gratitude_text === "" &&
    body.reflection_text === "" &&
    body.drained_tag_ids.length === 0 &&
    body.charged_tag_ids.length === 0;
  if (isEmpty) return { ...prev, input: null, tags: [] };

  const baseId =
    (prev.input?.id as string | undefined) ?? `temp-${prev.local_date}`;
  const nextTags: TagDayLink[] = [];
  for (const id of body.drained_tag_ids) {
    const t = tagsLookup.get(id);
    nextTags.push({ tag_id: id, label: t?.label ?? "…", role: "drainer" });
  }
  for (const id of body.charged_tag_ids) {
    const t = tagsLookup.get(id);
    nextTags.push({ tag_id: id, label: t?.label ?? "…", role: "charger" });
  }
  return {
    local_date: prev.local_date,
    tags: nextTags,
    input: {
      id: baseId,
      local_date: prev.local_date,
      mood: body.mood,
      drained_text: body.drained_text,
      charged_text: body.charged_text,
      gratitude_text: body.gratitude_text,
      reflection_text: body.reflection_text,
      backfilled: prev.input?.backfilled ?? false,
      edited_at: prev.input?.edited_at ?? null,
      created_at: prev.input?.created_at ?? new Date().toISOString(),
      updated_at: new Date().toISOString(),
    },
  };
}

// Today: PUT /api/daily/inputs. Optimistic: write straight into the
// cache, roll back on failure. Stats keys are invalidated so the
// SummariesPage chart updates next time the user navigates there.
export function useSaveDailyInput() {
  const qc = useQueryClient();
  return useMutation<
    DailyInputResponse | { deleted: boolean; local_date: string },
    ApiError,
    DailyInputUpsertBody,
    SaveContext
  >({
    mutationFn: (body) => saveDailyInput(body),
    onMutate: async (body) => {
      await qc.cancelQueries({ queryKey: dailyInputKey() });
      const prev = qc.getQueryData<DailyInputResponse>(dailyInputKey());
      const tagsLookup = readTagsLookup(qc);
      const next = buildOptimistic(body, prev, tagsLookup);
      if (next) qc.setQueryData<DailyInputResponse>(dailyInputKey(), next);
      return { prev };
    },
    onError: (err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(dailyInputKey(), ctx.prev);
      toast.error("Couldn't save", { description: err.message });
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: dailyInputKey() });
      qc.invalidateQueries({ queryKey: ["summaries", "stats"] });
      qc.invalidateQueries({ queryKey: heatmapKey() });
    },
  });
}

export function useUpdateDailyInput(localDate: string) {
  const qc = useQueryClient();
  return useMutation<
    DailyInputResponse | { deleted: boolean; local_date: string },
    ApiError,
    DailyInputUpsertBody,
    SaveContext
  >({
    mutationFn: (body) => updateDailyInputByDate(localDate, body),
    onMutate: async (body) => {
      await qc.cancelQueries({ queryKey: dailyInputKey(localDate) });
      const prev = qc.getQueryData<DailyInputResponse>(dailyInputKey(localDate));
      const tagsLookup = readTagsLookup(qc);
      const next = buildOptimistic(body, prev, tagsLookup);
      if (next) qc.setQueryData<DailyInputResponse>(dailyInputKey(localDate), next);
      return { prev };
    },
    onError: (err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(dailyInputKey(localDate), ctx.prev);
      toast.error("Couldn't save", { description: err.message });
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: dailyInputKey(localDate) });
      qc.invalidateQueries({ queryKey: ["summaries", "stats"] });
      qc.invalidateQueries({ queryKey: heatmapKey() });
    },
  });
}

// Reads cached tag lists into a single id→Tag map for optimistic
// updates. Falls back to an empty map when the cache hasn't seen any
// /api/tags responses yet.
//
// Imported from the same file as the cache to avoid a circular dep
// from a separate util module.
import type { QueryClient } from "@tanstack/react-query";
function readTagsLookup(qc: QueryClient): Map<string, Tag> {
  const lookup = new Map<string, Tag>();
  const queries = qc.getQueriesData<{ tags?: Tag[] }>({ queryKey: ["tags"] });
  for (const [, data] of queries) {
    for (const t of data?.tags ?? []) lookup.set(t.id, t);
  }
  return lookup;
}
