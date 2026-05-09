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

// SMART shaper SSE stream ----------------------------------------------

// DraftMessage is one turn of the shaper conversation. The FE owns the
// transcript and resends it each time it asks the server for the next
// assistant turn (the server is stateless for /goals/draft).
export interface DraftMessage {
  role: "user" | "assistant";
  content: string;
}

// CommitGoalArgs comes off the SMART shaper's `commit_goal` tool call.
// The FE captures it from the SSE 'tool' event and surfaces a "Save
// goal" CTA; clicking the CTA POSTs to /api/goals.
export interface CommitGoalArgs {
  title: string;
  check_in_question: string;
  duration_weeks: number;
}

export type DraftStreamEvent =
  | { kind: "token"; delta: string }
  | { kind: "tool"; name: string; args: Record<string, unknown> }
  | { kind: "error"; message: string }
  | { kind: "done" };

const DRAFT_BASE = (import.meta as { env: { VITE_API_BASE?: string } }).env.VITE_API_BASE ?? "";

// streamDraft opens the SSE for POST /api/goals/draft. Server returns
// one assistant turn per call. `opener=true` on the very first call
// (empty transcript) tells the server to inject the welcoming prompt.
export async function* streamDraft(
  messages: DraftMessage[],
  signal: AbortSignal,
  opener: boolean,
): AsyncGenerator<DraftStreamEvent> {
  const res = await fetch(`${DRAFT_BASE}/api/goals/draft`, {
    method: "POST",
    credentials: "include",
    signal,
    headers: {
      "Content-Type": "application/json",
      "X-Requested-With": "fetch",
      Accept: "text/event-stream",
    },
    body: JSON.stringify({ messages, opener }),
  });
  if (!res.ok) {
    let parsed: unknown = undefined;
    try {
      parsed = await res.json();
    } catch {
      /* leave undefined */
    }
    const msg =
      parsed && typeof parsed === "object" && "error" in parsed
        ? String((parsed as { error: unknown }).error)
        : `HTTP ${res.status}`;
    yield { kind: "error", message: msg };
    return;
  }
  if (!res.body) {
    yield { kind: "error", message: "no response body" };
    return;
  }
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buf = "";
  try {
    while (true) {
      if (signal.aborted) return;
      const { value, done } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      let idx;
      while ((idx = buf.indexOf("\n\n")) !== -1) {
        const frame = buf.slice(0, idx);
        buf = buf.slice(idx + 2);
        const ev = parseDraftFrame(frame);
        if (ev) yield ev;
      }
    }
  } finally {
    try {
      reader.releaseLock();
    } catch {
      /* ignore */
    }
  }
}

function parseDraftFrame(frame: string): DraftStreamEvent | null {
  let event = "";
  let data = "";
  for (const line of frame.split("\n")) {
    if (line.startsWith("event:")) event = line.slice(6).trim();
    else if (line.startsWith("data:")) data += line.slice(5).trim();
  }
  if (!event) return null;
  let parsed: Record<string, unknown> = {};
  try {
    parsed = data ? JSON.parse(data) : {};
  } catch {
    return { kind: "error", message: `malformed frame: ${data.slice(0, 80)}` };
  }
  switch (event) {
    case "token":
      return { kind: "token", delta: typeof parsed.delta === "string" ? parsed.delta : "" };
    case "tool":
      return {
        kind: "tool",
        name: typeof parsed.name === "string" ? parsed.name : "",
        args: (parsed.args as Record<string, unknown>) ?? {},
      };
    case "error":
      return {
        kind: "error",
        message: typeof parsed.message === "string" ? parsed.message : "stream error",
      };
    case "done":
      return { kind: "done" };
    default:
      return null;
  }
}
