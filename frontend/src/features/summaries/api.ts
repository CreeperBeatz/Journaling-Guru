import { api } from "@/api/client";

export type PeriodType = "day" | "week" | "month" | "year";

export interface SummaryMetadata {
  emotions?: string[];
  mood_score?: number;
  mood_label?: string;
  topics?: string[];
  entry_count?: number;
}

export interface Summary {
  id: string;
  period_type: PeriodType;
  period_start: string; // YYYY-MM-DD
  period_end: string; // YYYY-MM-DD
  body: string;
  metadata: SummaryMetadata;
  model: string;
  prompt_tokens: number;
  completion_tokens: number;
  generated_at: string;
}

export interface MoodPoint {
  local_date: string;
  score: number;
}

export interface EmotionCount {
  emotion: string;
  count: number;
}

export interface StatsResponse {
  window_days: number;
  mood: MoodPoint[];
  emotions: EmotionCount[];
}

export function listSummaries(period: PeriodType, limit?: number): Promise<{
  period: PeriodType;
  summaries: Summary[];
}> {
  const qs = new URLSearchParams({ period });
  if (limit) qs.set("limit", String(limit));
  return api(`/api/summaries?${qs.toString()}`);
}

export function getSummary(id: string): Promise<Summary> {
  return api(`/api/summaries/${encodeURIComponent(id)}`);
}

export function regenerateSummary(period_type: PeriodType, period_start: string): Promise<{
  triggered: boolean;
  period_type: PeriodType;
  period_start: string;
}> {
  return api("/api/summaries/regenerate", {
    method: "POST",
    body: { period_type, period_start },
  });
}

export function getStats(days?: number): Promise<StatsResponse> {
  const qs = days ? `?days=${days}` : "";
  return api(`/api/summaries/stats${qs}`);
}
