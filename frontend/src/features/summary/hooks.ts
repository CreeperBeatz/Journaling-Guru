import { useQuery } from "@tanstack/react-query";

import type { ApiError } from "@/api/client";
import type { JournalEntry } from "@/features/journal/api";

import { listEntriesByQuestion } from "./api";

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
