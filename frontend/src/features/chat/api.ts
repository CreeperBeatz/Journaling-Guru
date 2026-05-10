import { ApiError, api } from "@/api/client";

// Chat session phase mirrors backend domain.ChatPhase. Drives the UI
// state machine for the composer, wrap-up affordance, and overlay.
export type ChatPhase =
  | "greeting"
  | "exploring"
  | "wrapping_up"
  | "finalized"
  | "abandoned";

export type ChatMode = "text" | "voice";

export type ExtractionStatus =
  | "idle"
  | "pending"
  | "running"
  | "completed"
  | "failed";

export type ChatRole = "user" | "assistant" | "tool" | "system_event";

export interface ChatSession {
  id: string;
  local_date: string;
  mode: ChatMode;
  phase: ChatPhase;
  chat_model: string;
  extraction_model: string;
  openai_session_id?: string | null;
  started_at: string;
  last_activity_at: string;
  ended_at?: string | null;
  finalized_at?: string | null;
  extraction_status: ExtractionStatus;
  extraction_error?: string | null;
  /** Authoritative covered set written by the post-turn classifier.
   * Initial render reads this; the SSE coverage_update event replaces
   * it live during streaming. */
  covered_question_ids: string[];
  created_at: string;
  updated_at: string;
}

export interface ChatMessage {
  id: string;
  session_id: string;
  seq: number;
  role: ChatRole;
  content: string;
  tool_name?: string | null;
  tool_args?: Record<string, unknown> | null;
  tool_result?: Record<string, unknown> | null;
  token_in: number;
  token_out: number;
  created_at: string;
}

export interface ChatSessionEnvelope {
  session: ChatSession | null;
  messages: ChatMessage[];
}

export interface FinalizeResponse {
  session_id: string;
  extraction_status: ExtractionStatus;
  phase: ChatPhase;
  poll_status_url: string;
}

export interface ExtractionStatusResponse {
  status: ExtractionStatus;
  error?: string | null;
  finalized_at?: string | null;
  phase: ChatPhase;
}

export function getTodaySession(): Promise<ChatSessionEnvelope> {
  return api("/api/chat/sessions/today");
}

export function getSessionByDate(date: string): Promise<ChatSessionEnvelope> {
  return api(`/api/chat/sessions/by-date/${encodeURIComponent(date)}`);
}

export function createOrResumeSession(): Promise<ChatSessionEnvelope> {
  return api("/api/chat/sessions", { method: "POST", body: {} });
}

// finalizeSession schedules the extraction job. The worker silently
// merges extracted content into any pre-existing manual entries
// (LLM-merge for non-empty conflicts; manual fields preserved when
// nothing was extracted). No keep/replace fork.
export function finalizeSession(id: string): Promise<FinalizeResponse> {
  return api(`/api/chat/sessions/${encodeURIComponent(id)}/finalize`, {
    method: "POST",
    body: {},
  });
}

// ---------- voice (Phase 6b) ----------

export interface StartVoiceResponse {
  client_secret: string;
  expires_at: number;
  model: string;
  session_id: string;
  openai_session_id: string;
}

export function startVoiceSession(id: string): Promise<StartVoiceResponse> {
  return api(`/api/chat/sessions/${encodeURIComponent(id)}/voice/start`, {
    method: "POST",
    body: {},
  });
}

export interface AppendVoiceTranscriptResponse {
  message_id?: string;
  seq?: number;
  crisis?: boolean;
  resources_url?: string;
}

export function appendVoiceTranscript(
  id: string,
  payload: { role: "user" | "assistant"; content: string; client_seq: number },
): Promise<AppendVoiceTranscriptResponse> {
  return api(`/api/chat/sessions/${encodeURIComponent(id)}/voice/transcript`, {
    method: "POST",
    body: payload,
  });
}

export function getExtractionStatus(id: string): Promise<ExtractionStatusResponse> {
  return api(`/api/chat/sessions/${encodeURIComponent(id)}/extraction/status`);
}

// cancelWrapUp flips a wrapping_up session back to exploring. The
// server appends a `user_cancel_wrap_up` system_event so the LLM
// transcript reflects the change of heart on the next turn. Only
// valid while phase is wrapping_up; surface 409s as a no-op.
export function cancelWrapUp(id: string): Promise<{ phase: ChatPhase }> {
  return api(`/api/chat/sessions/${encodeURIComponent(id)}/wrap-up/cancel`, {
    method: "POST",
    body: {},
  });
}

// Reset wipes the session's transcript, rolls phase to greeting, and
// drops any pending extraction. Saved daily_inputs / journal_entries
// are NOT touched. The FE shows a destructive confirmation dialog
// before invoking — see ChatHeader.
export function resetSession(id: string): Promise<ChatSessionEnvelope> {
  return api(`/api/chat/sessions/${encodeURIComponent(id)}/reset`, {
    method: "POST",
    body: {},
  });
}

// ---------- SSE streaming ----------

// Server emits frames of the form `event: NAME\ndata: {...}\n\n`. We
// can't use EventSource because:
//   1. POST /messages takes a body (EventSource is GET-only).
//   2. We need the X-Requested-With header for CSRF.
//   3. We need credentials:'include' to ride the session cookie.
//
// So we use fetch() + ReadableStream and parse the frames manually.
// One generator drives both /messages (POST) and /opener (GET) — the
// frame schema is identical.
export type ChatStreamEvent =
  | { kind: "token"; delta: string }
  | { kind: "tool"; name: string; args: Record<string, unknown> }
  | { kind: "phase"; phase: ChatPhase }
  | { kind: "crisis"; reason: string; resources_url: string }
  | { kind: "coverage_update"; covered_question_ids: string[] }
  | { kind: "error"; message: string }
  | {
      kind: "done";
      assistant_message_id?: string;
      prompt_tokens?: number;
      completion_tokens?: number;
      finish_reason?: string;
      model?: string;
    };

const BASE = (import.meta as { env: { VITE_API_BASE?: string } }).env.VITE_API_BASE ?? "";

interface OpenStreamArgs {
  path: string;
  method: "GET" | "POST";
  body?: unknown;
  signal: AbortSignal;
}

async function openSseStream({ path, method, body, signal }: OpenStreamArgs): Promise<Response> {
  const url = path.startsWith("http") ? path : `${BASE}${path}`;
  const res = await fetch(url, {
    method,
    credentials: "include",
    signal,
    headers: {
      "Content-Type": "application/json",
      "X-Requested-With": "fetch",
      Accept: "text/event-stream",
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
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
    throw new ApiError(res.status, msg, parsed);
  }
  if (!res.body) {
    throw new ApiError(500, "no response body", null);
  }
  return res;
}

// streamMessage opens an SSE connection for POST /sessions/:id/messages
// and yields parsed events. The async generator finishes when the server
// closes the connection or the abort signal fires.
export async function* streamMessage(
  sessionId: string,
  content: string,
  signal: AbortSignal,
): AsyncGenerator<ChatStreamEvent> {
  const res = await openSseStream({
    path: `/api/chat/sessions/${encodeURIComponent(sessionId)}/messages`,
    method: "POST",
    body: { content },
    signal,
  });
  yield* parseSseStream(res.body!, signal);
}

// streamOpener opens GET /sessions/:id/opener for the streamed greeting.
export async function* streamOpener(
  sessionId: string,
  signal: AbortSignal,
): AsyncGenerator<ChatStreamEvent> {
  const res = await openSseStream({
    path: `/api/chat/sessions/${encodeURIComponent(sessionId)}/opener`,
    method: "GET",
    signal,
  });
  yield* parseSseStream(res.body!, signal);
}

// streamWrapUp opens POST /sessions/:id/wrap-up for the user-initiated
// closing pass. Server inserts a system_event into the transcript,
// advances phase to wrapping_up, and streams a single assistant turn
// covering remaining topics + a propose_wrap_up.
export async function* streamWrapUp(
  sessionId: string,
  signal: AbortSignal,
): AsyncGenerator<ChatStreamEvent> {
  const res = await openSseStream({
    path: `/api/chat/sessions/${encodeURIComponent(sessionId)}/wrap-up`,
    method: "POST",
    signal,
  });
  yield* parseSseStream(res.body!, signal);
}

async function* parseSseStream(
  body: ReadableStream<Uint8Array>,
  signal: AbortSignal,
): AsyncGenerator<ChatStreamEvent> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buf = "";
  try {
    while (true) {
      if (signal.aborted) return;
      const { value, done } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      // Split on the SSE frame separator (\n\n). Trailing partial frame
      // stays in buf for the next iteration.
      let idx;
      while ((idx = buf.indexOf("\n\n")) !== -1) {
        const frame = buf.slice(0, idx);
        buf = buf.slice(idx + 2);
        const ev = parseFrame(frame);
        if (ev) yield ev;
      }
    }
  } finally {
    try {
      reader.releaseLock();
    } catch {
      /* swallow — already released on abort */
    }
  }
}

function parseFrame(frame: string): ChatStreamEvent | null {
  let event = "";
  let data = "";
  for (const line of frame.split("\n")) {
    if (line.startsWith("event:")) event = line.slice(6).trim();
    else if (line.startsWith("data:")) data += line.slice(5).trim();
  }
  if (!event || !data) return null;
  let parsed: Record<string, unknown> = {};
  try {
    parsed = JSON.parse(data);
  } catch {
    // Malformed frame — surface as an error event so the UI can react.
    return { kind: "error", message: `malformed SSE frame: ${data.slice(0, 80)}` };
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
    case "phase":
      return { kind: "phase", phase: parsed.phase as ChatPhase };
    case "coverage_update":
      return {
        kind: "coverage_update",
        covered_question_ids: Array.isArray(parsed.covered_question_ids)
          ? (parsed.covered_question_ids as unknown[]).filter(
              (x): x is string => typeof x === "string",
            )
          : [],
      };
    case "crisis":
      return {
        kind: "crisis",
        reason: typeof parsed.reason === "string" ? parsed.reason : "self_harm_signal",
        resources_url: typeof parsed.resources_url === "string" ? parsed.resources_url : "/resources",
      };
    case "error":
      return {
        kind: "error",
        message: typeof parsed.message === "string" ? parsed.message : "stream error",
      };
    case "done":
      return {
        kind: "done",
        assistant_message_id:
          typeof parsed.assistant_message_id === "string"
            ? parsed.assistant_message_id
            : undefined,
        prompt_tokens:
          typeof parsed.prompt_tokens === "number" ? parsed.prompt_tokens : undefined,
        completion_tokens:
          typeof parsed.completion_tokens === "number" ? parsed.completion_tokens : undefined,
        finish_reason:
          typeof parsed.finish_reason === "string" ? parsed.finish_reason : undefined,
        model: typeof parsed.model === "string" ? parsed.model : undefined,
      };
    default:
      return null;
  }
}
