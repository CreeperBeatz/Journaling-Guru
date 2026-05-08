import { api } from "@/api/client";

export interface Question {
  id: string;
  prompt: string;
  position: number;
  created_at: string;
  updated_at: string;
}

export interface JournalEntry {
  id: string;
  question_id: string;
  local_date: string; // YYYY-MM-DD
  body: string;
  source: "text" | "voice" | "chat";
  chat_session_id?: string | null;
  created_at: string;
  updated_at: string;
}

export interface DayEntries {
  local_date: string;
  entries: JournalEntry[];
}

export interface DateSummary {
  local_date: string;
  entry_count: number;
}

export interface HeatmapDay {
  local_date: string;
  answered: number;
  chat_turns: number;
  mood?: number | null;
}

export interface HeatmapResponse {
  from: string;
  to: string;
  today: string;
  days: HeatmapDay[];
}

export function listQuestions(): Promise<{ questions: Question[] }> {
  return api("/api/questions");
}

export function createQuestion(prompt: string): Promise<Question> {
  return api("/api/questions", { method: "POST", body: { prompt } });
}

export function updateQuestion(id: string, prompt: string): Promise<Question> {
  return api(`/api/questions/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: { prompt },
  });
}

export function archiveQuestion(id: string): Promise<{ ok: boolean }> {
  return api(`/api/questions/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export function reorderQuestions(ids: string[]): Promise<{ ok: boolean }> {
  return api("/api/questions/reorder", { method: "POST", body: { ids } });
}

export function listEntries(date?: string): Promise<DayEntries> {
  const qs = date ? `?date=${encodeURIComponent(date)}` : "";
  return api(`/api/entries${qs}`);
}

export function listEntryDates(limit?: number): Promise<{ dates: DateSummary[] }> {
  const qs = limit && limit > 0 ? `?limit=${limit}` : "";
  return api(`/api/entries/dates${qs}`);
}

export function getHeatmap(from?: string, to?: string): Promise<HeatmapResponse> {
  const params = new URLSearchParams();
  if (from) params.set("from", from);
  if (to) params.set("to", to);
  const qs = params.toString();
  return api(`/api/history/heatmap${qs ? `?${qs}` : ""}`);
}

// Upserts today's entry. Empty body deletes the row, matching backend.
export function saveEntry(question_id: string, body: string): Promise<JournalEntry | { deleted: boolean; local_date: string }> {
  return api("/api/entries", { method: "PUT", body: { question_id, body } });
}

// Edits an existing entry by id. Used by HistoryView to amend past days
// without trusting a client-provided date.
export function updateEntry(id: string, body: string): Promise<JournalEntry | { deleted: boolean }> {
  return api(`/api/entries/${encodeURIComponent(id)}`, { method: "PATCH", body: { body } });
}
