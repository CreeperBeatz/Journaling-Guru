import { useQuery } from "@tanstack/react-query";

import type { ApiError } from "@/api/client";
import type { JournalEntry } from "@/features/journal/api";

import {
  Zone1Response,
  Zone2Response,
  Zone3Response,
  getZone1,
  getZone2,
  getZone3,
  listEntriesByQuestion,
} from "./api";

export const ENTRIES_BY_QUESTION_KEY = (questionId: string) =>
  ["summary", "by-question", questionId] as const;

export function useEntriesByQuestion(questionId: string | null) {
  return useQuery<JournalEntry[], ApiError>({
    queryKey: ENTRIES_BY_QUESTION_KEY(questionId ?? "__none"),
    enabled: !!questionId,
    queryFn: async () =>
      (await listEntriesByQuestion(questionId!)).entries,
    staleTime: 60_000,
  });
}

// Energy Audit zone hooks ------------------------------------------------

export const summaryZone1Key = ["summary", "zone1"] as const;
export const summaryZone2Key = (days: number) =>
  ["summary", "zone2", days] as const;
export const summaryZone3Key = ["summary", "zone3"] as const;

export function useSummaryZone1() {
  return useQuery<Zone1Response, ApiError>({
    queryKey: summaryZone1Key,
    queryFn: getZone1,
    staleTime: 60_000,
  });
}

export function useSummaryZone2(days = 30) {
  return useQuery<Zone2Response, ApiError>({
    queryKey: summaryZone2Key(days),
    queryFn: () => getZone2(days),
    staleTime: 60_000,
  });
}

export function useSummaryZone3() {
  return useQuery<Zone3Response, ApiError>({
    queryKey: summaryZone3Key,
    queryFn: getZone3,
    staleTime: 60_000,
  });
}
