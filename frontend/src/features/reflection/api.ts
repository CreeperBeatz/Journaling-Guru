import { api } from "@/api/client";

import type { ChatSessionEnvelope } from "@/features/chat/api";
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

// ReflectionTheme — one ad-hoc tag cluster surfaced by the weekly LLM
// synthesis. Themes are regenerated per week; they do NOT correspond
// to a persistent parent_tag_id.
export interface ReflectionTheme {
  name: string;
  tags: string[];
  role: "drainer" | "charger" | "mixed";
  days_appeared: number;
  note: string;
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

  // Weekly synthesis from the LLM. Empty/[] for pre-feature weeks or
  // weeks where the job hasn't fired yet (in which case
  // synthesis_pending is true and the FE shows an "arriving" affordance).
  //
  // Structured paragraphs (charged/drained/grateful/insights) are the
  // Sonnet-tier shape; `letter` is the legacy single-string fallback
  // for rows synthesised before the structured prompt landed.
  letter: string;
  charged: string;
  drained: string;
  grateful: string;
  insights: string;
  themes: ReflectionTheme[];
  closing_question: string;
  synthesis_pending: boolean;

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

// patchReflectionByWeek is the historical-edit counterpart — same
// partial-update shape, but targets a specific past week. Used by the
// History view's editable surprise_text + goal_notes textareas.
export function patchReflectionByWeek(
  weekStart: string,
  body: PatchReflectionBody,
): Promise<ReflectionResponse> {
  return api(`/api/reflection/by-week/${weekStart}`, { method: "PATCH", body });
}

export function completeReflection(): Promise<ReflectionResponse> {
  return api("/api/reflection/this-week/complete", { method: "POST" });
}

export function replayReflection(): Promise<ReflectionResponse> {
  return api("/api/reflection/this-week/replay", { method: "POST" });
}

// ----- Weekly reflection chat -----

// getThisWeekChat returns the weekly-scoped chat envelope; `session` is
// null when no chat has been created for this week yet.
export function getThisWeekChat(): Promise<ChatSessionEnvelope> {
  return api("/api/reflection/this-week/chat");
}

// createOrResumeWeeklyChat lazily creates the weekly chat session (or
// returns the existing one). Idempotent server-side.
export function createOrResumeWeeklyChat(): Promise<ChatSessionEnvelope> {
  return api("/api/reflection/this-week/chat", { method: "POST", body: {} });
}

// getReflectionChatByWeek loads the historical read-only weekly chat
// transcript for a past week.
export function getReflectionChatByWeek(weekStart: string): Promise<ChatSessionEnvelope> {
  return api(`/api/reflection/by-week/${weekStart}/chat`);
}

// SystemEventContent is the closed set of whitelisted event strings the
// backend's POST /sessions/:id/system-event endpoint accepts.
export type SystemEventContent =
  | "user_accepted_goal"
  | "user_declined_goal"
  | "user_edited_goal"
  | "user_accepted_extend_goal"
  | "user_declined_extend_goal"
  | "user_accepted_complete_goal"
  | "user_declined_complete_goal";

// SystemEventMeta is the closed-key payload the FE may attach to a
// system_event. Lets the assistant see which goal a decision applied
// to on later turns — without this, a session that proposes multiple
// goals has no memory of which were accepted/declined. Server
// whitelists keys and caps each value at 200 chars; unknown keys are
// dropped silently.
export interface SystemEventMeta {
  goal_id?: string;
  goal_title?: string;
  outcome?: "kept" | "dropped" | "inconclusive";
  weeks?: string; // string for symmetry with the server's map[string]string
}

export function postSystemEvent(
  sessionId: string,
  content: SystemEventContent,
  meta?: SystemEventMeta,
): Promise<{ message_id: string; seq: number }> {
  return api(`/api/chat/sessions/${sessionId}/system-event`, {
    method: "POST",
    body: meta ? { content, meta } : { content },
  });
}
