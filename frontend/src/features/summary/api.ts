import { api } from "@/api/client";

import type { JournalEntry } from "@/features/journal/api";
import type { Goal } from "@/features/goals/api";

export function listEntriesByQuestion(
  questionId: string,
  limit?: number,
): Promise<{ entries: JournalEntry[] }> {
  const qs = limit && limit > 0 ? `?limit=${limit}` : "";
  return api(
    `/api/questions/${encodeURIComponent(questionId)}/entries${qs}`,
  );
}

// ---------- Energy Audit summary surface (Phase 6) ----------

export interface DailyMoodPoint {
  local_date: string;
  score: number;
}

export interface Zone1GoalStatus {
  id: string;
  title: string;
  start_date: string;
  end_date: string;
  day_index: number;
  total_days: number;
  kept_count: number;
  answered_count: number;
  // Motivation captured at goal creation — empty for goals that were
  // created before propose_goal / the SMART shaper captured these.
  why_matters: string;
  if_followed: string;
  if_not_followed: string;
}

export interface Zone1Response {
  window_days: number;
  baseline_days_required: number;
  has_baseline: boolean;
  mood: DailyMoodPoint[];
  mood_avg_7d: number | null;
  mood_avg_prior_7d: number | null;
  headline: string | null;
  headline_fallback: string;
  active_goals: Zone1GoalStatus[];
}

export interface TagAggregate {
  tag_id: string;
  label: string;
  appearances: number;
  avg_mood: number | null;
}

export interface Zone2Response {
  window_days: number;
  drainers: TagAggregate[];
  chargers: TagAggregate[];
}

export interface Zone3Response {
  goals: Goal[];
}

export function getZone1(): Promise<Zone1Response> {
  return api("/api/summary/zone1");
}

export function getZone2(days = 30): Promise<Zone2Response> {
  return api(`/api/summary/zone2?days=${days}`);
}

export function getZone3(): Promise<Zone3Response> {
  return api("/api/summary/zone3");
}
