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
}

export function getThisWeekReflection(): Promise<ReflectionResponse> {
  return api("/api/reflection/this-week");
}
