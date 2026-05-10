import { api } from "@/api/client";

import type { Zone1GoalStatus } from "@/features/summary/api";

export interface ReflectionTagRow {
  tag_id: string;
  label: string;
  appearances: number;
  avg_mood: number | null;
  delta_vs_prior: number;
}

export interface ReflectionGratitude {
  local_date: string;
  text: string;
}

// ReflectionResponse mirrors handlers/summaries.go::ReflectionResponse.
// Wizard-state fields (started/step/...) are only populated for the
// current week; history reads (`/by-week/...`) leave them as defaults.
export interface ReflectionResponse {
  week_start: string;
  week_end: string;
  prior_week_start: string;
  prior_week_end: string;
  mood_avg: number | null;
  mood_avg_prior: number | null;
  entry_count: number;
  drainers: ReflectionTagRow[];
  chargers: ReflectionTagRow[];
  gratitude_items: ReflectionGratitude[];
  active_goals: Zone1GoalStatus[];

  // Wizard state.
  started: boolean;
  step: number;            // 1..3 once started; 0 when not started
  surprise_text: string;
  goal_notes: Record<string, string>;
  // Goal IDs shaped during this reflection's Card 3 — drives the Done
  // page split between "Active" (carried over) and "New" (this week).
  new_goal_ids: string[];
  completed_at: string | null;
}

export function getThisWeekReflection(): Promise<ReflectionResponse> {
  return api("/api/reflection/this-week");
}

export function getReflectionByWeek(weekStart: string): Promise<ReflectionResponse> {
  return api(`/api/reflection/by-week/${weekStart}`);
}

export function startReflection(): Promise<ReflectionResponse> {
  return api("/api/reflection/this-week/start", { method: "POST" });
}

// PatchReflectionBody — at least one of surprise_text / step / (goal_id +
// goal_note) should be supplied; mixing is fine. Empty goal_note clears
// the entry for that goal_id.
export interface PatchReflectionBody {
  surprise_text?: string;
  step?: number;
  goal_id?: string;
  goal_note?: string;
  // Append a freshly-created goal_id to new_goal_ids (deduped server-side).
  new_goal_id?: string;
}

export function patchReflection(body: PatchReflectionBody): Promise<ReflectionResponse> {
  return api("/api/reflection/this-week", { method: "PATCH", body });
}

export function completeReflection(): Promise<ReflectionResponse> {
  return api("/api/reflection/this-week/complete", { method: "POST" });
}
