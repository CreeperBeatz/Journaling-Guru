import { api } from "@/api/client";

import type { JournalEntry } from "@/features/journal/api";

export function listEntriesByQuestion(
  questionId: string,
  limit?: number,
): Promise<{ entries: JournalEntry[] }> {
  const qs = limit && limit > 0 ? `?limit=${limit}` : "";
  return api(
    `/api/questions/${encodeURIComponent(questionId)}/entries${qs}`,
  );
}
