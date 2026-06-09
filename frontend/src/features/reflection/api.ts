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

  // Monthly — non-null when this week hosts a monthly reflection (the
  // first reflection day on-or-after a calendar month end, plus one
  // carry-over grace week). The wizard gains a monthly-letter sheet and
  // a life check-in step; the chat zooms out to the month.
  monthly: MonthlyReflectionBlock | null;
}

// MonthlyReflectionBlock mirrors handlers/summaries.go::MonthlyReflectionBlock.
export interface MonthlyReflectionBlock {
  month_start: string;
  month_end: string;

  // Monthly letter (period_type='month' summaries row).
  headline: string;
  arc: string;
  recurring: string;
  goals_retro: string;
  closing_question: string;
  synthesis_pending: boolean;

  // Month state.
  intention_text: string;
  direction_text: string;
  last_month_intention: string;
  ratings: Record<string, number> | null;      // null until the check-in is submitted
  rating_notes: Record<string, string> | null; // optional per-domain "why this score?"
  prev_ratings: Record<string, number> | null; // last completed month's, for ghost dots
  ratings_set_at: string | null;
  completed_at: string | null;
}

// LIFE_DOMAINS mirrors backend/internal/domain/life_domains.go — keys
// are stable (they live inside monthly_reflections.ratings jsonb and
// the yearly chart depends on them); labels are display-only. Order
// matters: the global item renders first (PWI/OECD ordering), the
// optional Belonging item last, collapsed by default.
export interface LifeDomain {
  key: string;
  label: string;
  question: string;
  optional?: boolean;
}

export const LIFE_DOMAINS: LifeDomain[] = [
  { key: "life_overall", label: "Life as a whole", question: "Taking everything together — how satisfied are you with your life as a whole these days?" },
  { key: "health_energy", label: "Health & energy", question: "Your health, and how your body feels day to day?" },
  { key: "mind_inner", label: "Mind & inner life", question: "How you've been feeling inside — your mood, calm, and self-kindness?" },
  { key: "relationships", label: "Close relationships", question: "Your relationships with the people closest to you?" },
  { key: "work_purpose", label: "Work & purpose", question: "What you spend your days doing, and what you're working toward?" },
  { key: "money_security", label: "Money & security", question: "Your finances, and how secure you feel about the future?" },
  { key: "play_rest", label: "Play & rest", question: "The time you get for fun, rest, and things you enjoy?" },
  { key: "growth_learning", label: "Growth & learning", question: "How you're growing — learning, creating, becoming who you want to be?" },
  { key: "belonging", label: "Belonging", question: "Feeling part of something beyond yourself — community, nature, or spirituality?", optional: true },
];

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

// ----- Monthly reflection -----

// setMonthlyIntention persists the user's intention for the month the
// current week hosts (404 on plain weeks). Returns the rebuilt
// ReflectionResponse so the cache swaps in one motion.
export function setMonthlyIntention(intentionText: string): Promise<ReflectionResponse> {
  return api("/api/reflection/this-month/intention", {
    method: "POST",
    body: { intention_text: intentionText },
  });
}

// setMonthlyRatings persists the life check-in (0..10 score per domain
// key from LIFE_DOMAINS, plus an optional free-text note per domain).
// Partial maps are fine — Belonging is opt-in.
export function setMonthlyRatings(
  ratings: Record<string, number>,
  notes?: Record<string, string>,
): Promise<ReflectionResponse> {
  return api("/api/reflection/this-month/ratings", {
    method: "POST",
    body: notes && Object.keys(notes).length > 0 ? { ratings, notes } : { ratings },
  });
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
  | "user_declined_complete_goal"
  | "user_accepted_intention"
  | "user_declined_intention"
  | "user_edited_intention";

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
  intention_text?: string;
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
