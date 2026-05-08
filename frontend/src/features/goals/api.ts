import { api } from "@/api/client";

// Goal — the user's commit to a change, measured by a daily yes/no
// check-in. IDs are stable across the active → completed/abandoned
// transition; ending a goal flips status + outcome but keeps the title
// and history intact for Zone-3 (the "what I tried" ledger).
export interface Goal {
  id: string;
  title: string;
  check_in_question: string;
  start_date: string; // YYYY-MM-DD
  end_date: string;   // YYYY-MM-DD
  status: "active" | "completed" | "abandoned";
  outcome?: "kept" | "dropped" | "inconclusive" | null;
  conclusion_text: string;
  created_at: string;
  ended_at?: string | null;
}

export interface GoalCheckIn {
  goal_id: string;
  local_date: string;
  value: boolean;
  created_at: string;
  updated_at: string;
}

// ActiveGoalsResponse comes back from GET /api/goals?status=active. The
// `todays_check_ins` map gives the FE pre-filled yes/no answers without
// a second round-trip — keys are goal IDs, values are booleans.
export interface ActiveGoalsResponse {
  goals: Goal[];
  todays_check_ins: Record<string, boolean>;
  local_date: string;
}

export interface AllGoalsResponse {
  goals: Goal[];
}

export function listActiveGoals(): Promise<ActiveGoalsResponse> {
  return api("/api/goals?status=active");
}

export function listAllGoals(): Promise<AllGoalsResponse> {
  return api("/api/goals");
}

export interface CreateGoalBody {
  title: string;
  check_in_question: string;
  end_date: string;          // YYYY-MM-DD
  start_date?: string;       // optional; server defaults to today
}

export function createGoal(body: CreateGoalBody): Promise<Goal> {
  return api("/api/goals", { method: "POST", body });
}

export function completeGoal(
  id: string,
  outcome: "kept" | "dropped" | "inconclusive",
  conclusionText: string,
): Promise<Goal> {
  return api(`/api/goals/${id}`, {
    method: "PATCH",
    body: { action: "complete", outcome, conclusion_text: conclusionText },
  });
}

export function abandonGoal(id: string, conclusionText: string): Promise<Goal> {
  return api(`/api/goals/${id}`, {
    method: "PATCH",
    body: { action: "abandon", conclusion_text: conclusionText },
  });
}

export function checkInGoal(id: string, value: boolean): Promise<GoalCheckIn> {
  return api(`/api/goals/${id}/check-ins`, {
    method: "POST",
    body: { value },
  });
}
