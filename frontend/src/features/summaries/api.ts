import { api } from "@/api/client";

export type PeriodType = "day" | "week" | "month" | "year";

export interface SummaryTheme {
  name: string;
  tags: string[];
  role: "drainer" | "charger" | "mixed";
  days_appeared: number;
  note: string;
}

export interface SummaryMetadata {
  emotions?: string[];
  mood_score?: number;
  mood_label?: string;
  topics?: string[];
  entry_count?: number;
  // Weekly synthesis (Sonnet-tier prompt). `letter` is the legacy
  // single-blob fallback; the four paragraphs are the structured shape.
  letter?: string;
  charged?: string;
  drained?: string;
  grateful?: string;
  insights?: string;
  themes?: SummaryTheme[];
  closing_question?: string;
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

// Mirrors the lifecycle vocabulary in 0004_summaries.sql. `pending` = queued,
// `claimed` = dispatcher handed it to River, `completed`/`skipped` = done,
// `failed` = River exhausted retries.
export type SummaryJobStatus =
  | "pending"
  | "claimed"
  | "completed"
  | "skipped"
  | "failed";

export interface SummaryJob {
  id: string;
  period_type: PeriodType;
  period_start: string;
  fire_at: string;
  fired_at?: string;
  status: SummaryJobStatus;
  attempts: number;
  last_error?: string;
  created_at: string;
  updated_at: string;
}

// 404 when no job has ever existed for this period — caller treats that as
// "no in-flight regen". Resolved into `null` by the hook layer.
export function getSummaryJobStatus(
  period_type: PeriodType,
  period_start: string,
): Promise<SummaryJob> {
  const qs = new URLSearchParams({ period_type, period_start });
  return api(`/api/summaries/jobs/status?${qs.toString()}`);
}
