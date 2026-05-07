import { api } from "@/api/client";

// ClassifiedEmotion is one entry from the LLM Plutchik classifier.
// Base is one of the 8 wheel base emotions; subtype is one of the 24
// intensity-leveled subtypes. Surfaced via SummaryDetail / stats panel,
// NOT echoed back into the DailyInputs textarea — the user sees only
// their own raw text on the check-in surface.
export interface ClassifiedEmotion {
  base: string;
  subtype: string;
  raw_phrase: string;
}

// DailyInput is the user-provided per-day check-in. Distinct from
// JournalEntry (one row per question per day) — exactly one DailyInput
// per (user, local_date). Mood is a 1..10 score (null = unset).
export interface DailyInput {
  id: string;
  local_date: string; // YYYY-MM-DD
  mood_score: number | null;
  emotions_text: string;
  classified_emotions: ClassifiedEmotion[];
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
  emotions_text: string;
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
