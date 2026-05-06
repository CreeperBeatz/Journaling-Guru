import { api } from "@/api/client";

// DailyInput is the user-provided per-day check-in. Distinct from
// JournalEntry (one row per question per day) — exactly one DailyInput
// per (user, local_date). Mood is a 1..10 score (null = unset).
export interface DailyInput {
  id: string;
  local_date: string; // YYYY-MM-DD
  mood_score: number | null;
  emotions: string[];
  notes: string;
  created_at: string;
  updated_at: string;
}

export interface DailyInputResponse {
  local_date: string;
  input: DailyInput | null;
}

export interface DailyInputUpsertBody {
  mood_score: number | null;
  emotions: string[];
  notes: string;
}

export function getDailyInput(date?: string): Promise<DailyInputResponse> {
  const qs = date ? `?date=${encodeURIComponent(date)}` : "";
  return api(`/api/daily/inputs${qs}`);
}

// Upserts today's daily input. Server resolves "today" from the user's
// timezone + day_start_minutes — same convention as /api/entries.
export function saveDailyInput(body: DailyInputUpsertBody): Promise<
  DailyInput | { deleted: boolean; local_date: string }
> {
  return api("/api/daily/inputs", { method: "PUT", body });
}

// Edits a past day's check-in. HistoryView calls this to amend
// yesterday's mood after the day-start cutoff has rolled over.
export function updateDailyInputByDate(
  date: string,
  body: DailyInputUpsertBody,
): Promise<DailyInput | { deleted: boolean; local_date: string }> {
  return api(`/api/daily/inputs/by-date/${encodeURIComponent(date)}`, {
    method: "PATCH",
    body,
  });
}
