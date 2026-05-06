import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { ApiError } from "@/api/client";
import { toast } from "@/components/ui/sonner";

import {
  archiveQuestion,
  createQuestion,
  DayEntries,
  DateSummary,
  JournalEntry,
  listEntries,
  listEntryDates,
  listQuestions,
  Question,
  reorderQuestions,
  saveEntry,
  updateEntry,
  updateQuestion,
} from "./api";

export const QUESTIONS_KEY = ["questions"] as const;
export const entriesKey = (date?: string) =>
  date ? (["entries", date] as const) : (["entries", "today"] as const);
export const ENTRY_DATES_KEY = ["entries", "dates"] as const;

// Helper for stable temp ids during optimistic mutations. randomUUID is
// available everywhere we ship (modern browsers + service workers).
function tempId(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return `temp-${crypto.randomUUID()}`;
  }
  return `temp-${Math.random().toString(36).slice(2)}-${Date.now()}`;
}

export function useQuestions() {
  return useQuery<Question[], ApiError>({
    queryKey: QUESTIONS_KEY,
    queryFn: async () => (await listQuestions()).questions,
    staleTime: 5 * 60_000,
  });
}

export function useEntries(date?: string) {
  // Past dates are immutable from cache POV; explicit invalidates on edit
  // handle the rare update path. Today stays at 30s.
  const isPast = !!date;
  return useQuery<DayEntries, ApiError>({
    queryKey: entriesKey(date),
    queryFn: () => listEntries(date),
    staleTime: isPast ? Infinity : 30_000,
    gcTime: 30 * 60_000,
  });
}

export function useEntryDates(limit?: number) {
  return useQuery<DateSummary[], ApiError>({
    queryKey: ENTRY_DATES_KEY,
    queryFn: async () => (await listEntryDates(limit)).dates,
    staleTime: 60_000,
  });
}

// ---------- Question mutations (optimistic) ----------

export function useCreateQuestion() {
  const qc = useQueryClient();
  return useMutation<Question, ApiError, string, { prev?: Question[] }>({
    mutationFn: (prompt) => createQuestion(prompt),
    onMutate: async (prompt) => {
      await qc.cancelQueries({ queryKey: QUESTIONS_KEY });
      const prev = qc.getQueryData<Question[]>(QUESTIONS_KEY);
      const placeholder: Question = {
        id: tempId(),
        prompt,
        position: prev ? prev.length : 0,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      };
      qc.setQueryData<Question[]>(QUESTIONS_KEY, (old) => [...(old ?? []), placeholder]);
      return { prev };
    },
    onError: (err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(QUESTIONS_KEY, ctx.prev);
      toast.error("Couldn't add question", { description: err.message });
    },
    onSettled: () => qc.invalidateQueries({ queryKey: QUESTIONS_KEY }),
  });
}

export function useUpdateQuestion() {
  const qc = useQueryClient();
  return useMutation<
    Question,
    ApiError,
    { id: string; prompt: string },
    { prev?: Question[] }
  >({
    mutationFn: ({ id, prompt }) => updateQuestion(id, prompt),
    onMutate: async ({ id, prompt }) => {
      await qc.cancelQueries({ queryKey: QUESTIONS_KEY });
      const prev = qc.getQueryData<Question[]>(QUESTIONS_KEY);
      qc.setQueryData<Question[]>(QUESTIONS_KEY, (old) =>
        (old ?? []).map((q) =>
          q.id === id ? { ...q, prompt, updated_at: new Date().toISOString() } : q,
        ),
      );
      return { prev };
    },
    onError: (err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(QUESTIONS_KEY, ctx.prev);
      toast.error("Couldn't update question", { description: err.message });
    },
    onSettled: () => qc.invalidateQueries({ queryKey: QUESTIONS_KEY }),
  });
}

export function useArchiveQuestion() {
  const qc = useQueryClient();
  return useMutation<{ ok: boolean }, ApiError, string, { prev?: Question[] }>({
    mutationFn: (id) => archiveQuestion(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: QUESTIONS_KEY });
      const prev = qc.getQueryData<Question[]>(QUESTIONS_KEY);
      qc.setQueryData<Question[]>(QUESTIONS_KEY, (old) => (old ?? []).filter((q) => q.id !== id));
      return { prev };
    },
    onError: (err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(QUESTIONS_KEY, ctx.prev);
      toast.error("Couldn't archive question", { description: err.message });
    },
    onSettled: () => qc.invalidateQueries({ queryKey: QUESTIONS_KEY }),
  });
}

export function useReorderQuestions() {
  const qc = useQueryClient();
  return useMutation<{ ok: boolean }, ApiError, string[], { prev?: Question[] }>({
    mutationFn: (ids) => reorderQuestions(ids),
    onMutate: async (ids) => {
      await qc.cancelQueries({ queryKey: QUESTIONS_KEY });
      const prev = qc.getQueryData<Question[]>(QUESTIONS_KEY);
      qc.setQueryData<Question[]>(QUESTIONS_KEY, (old) => {
        if (!old) return old;
        const byId = new Map(old.map((q) => [q.id, q] as const));
        return ids
          .map((id, idx) => {
            const q = byId.get(id);
            return q ? { ...q, position: idx } : null;
          })
          .filter((q): q is Question => q !== null);
      });
      return { prev };
    },
    onError: (err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(QUESTIONS_KEY, ctx.prev);
      toast.error("Couldn't reorder", { description: err.message });
    },
    onSettled: () => qc.invalidateQueries({ queryKey: QUESTIONS_KEY }),
  });
}

// ---------- Entry mutations (optimistic) ----------

export function useSaveEntry() {
  const qc = useQueryClient();
  return useMutation<
    JournalEntry | { deleted: boolean; local_date: string },
    ApiError,
    { questionId: string; body: string },
    { prev?: DayEntries }
  >({
    mutationFn: ({ questionId, body }) => saveEntry(questionId, body),
    onMutate: async ({ questionId, body }) => {
      await qc.cancelQueries({ queryKey: entriesKey() });
      const prev = qc.getQueryData<DayEntries>(entriesKey());
      qc.setQueryData<DayEntries>(entriesKey(), (old) => {
        if (!old) return old;
        const idx = old.entries.findIndex((e) => e.question_id === questionId);
        if (body === "") {
          return idx === -1
            ? old
            : { ...old, entries: old.entries.filter((_, i) => i !== idx) };
        }
        if (idx === -1) {
          const placeholder: JournalEntry = {
            id: `temp-${questionId}`,
            question_id: questionId,
            local_date: old.local_date,
            body,
            source: "text",
            created_at: new Date().toISOString(),
            updated_at: new Date().toISOString(),
          };
          return { ...old, entries: [...old.entries, placeholder] };
        }
        return {
          ...old,
          entries: old.entries.map((e, i) =>
            i === idx ? { ...e, body, updated_at: new Date().toISOString() } : e,
          ),
        };
      });
      return { prev };
    },
    onError: (err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(entriesKey(), ctx.prev);
      toast.error("Couldn't save", {
        description: err.message ?? "We'll keep your draft. Try again?",
      });
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: entriesKey() });
      qc.invalidateQueries({ queryKey: ENTRY_DATES_KEY });
    },
  });
}

export function useUpdateEntry(localDate: string) {
  const qc = useQueryClient();
  return useMutation<
    JournalEntry | { deleted: boolean },
    ApiError,
    { id: string; body: string },
    { prev?: DayEntries }
  >({
    mutationFn: ({ id, body }) => updateEntry(id, body),
    onMutate: async ({ id, body }) => {
      await qc.cancelQueries({ queryKey: entriesKey(localDate) });
      const prev = qc.getQueryData<DayEntries>(entriesKey(localDate));
      qc.setQueryData<DayEntries>(entriesKey(localDate), (old) => {
        if (!old) return old;
        if (body === "") {
          return { ...old, entries: old.entries.filter((e) => e.id !== id) };
        }
        return {
          ...old,
          entries: old.entries.map((e) =>
            e.id === id ? { ...e, body, updated_at: new Date().toISOString() } : e,
          ),
        };
      });
      return { prev };
    },
    onError: (err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(entriesKey(localDate), ctx.prev);
      toast.error("Couldn't save", { description: err.message });
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: entriesKey(localDate) });
      qc.invalidateQueries({ queryKey: entriesKey() });
      qc.invalidateQueries({ queryKey: ENTRY_DATES_KEY });
    },
  });
}
