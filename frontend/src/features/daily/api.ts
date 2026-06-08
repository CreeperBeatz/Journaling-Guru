import { api } from "@/api/client";

// Tag — user-owned valenced label for a recurring drainer or charger.
// IDs are permanent; rename updates label only so day → tag history
// stays intact across renames.
export interface Tag {
  id: string;
  label: string;
  valence: "positive" | "negative" | "neutral";
  status: "active" | "merged" | "archived";
  merged_into_tag_id?: string | null;
  created_at: string;
  updated_at: string;
}

// TagDayLink — one (tag, role) pair attached to a day. Returned by GET
// /api/daily/inputs alongside the input row; the UI splits by role into
// drainer / charger pill rows.
export interface TagDayLink {
  tag_id: string;
  label: string;
  role: "drainer" | "charger";
}

// DailyInput is the user-provided per-day check-in under the Energy
// Audit pivot. Five fixed fields plus drainer/charger tag attachments
// (which live in the parallel `tags` array on the response, not on the
// DailyInput row itself).
//
// Mood is a signed -2..+2 scale (-2=very bad, -1=bad, 0=neutral,
// +1=good, +2=very good). null = unset.
export interface DailyInput {
  id: string;
  local_date: string; // YYYY-MM-DD
  mood: number | null;
  drained_text: string;
  charged_text: string;
  gratitude_text: string;
  reflection_text: string;
  backfilled: boolean;
  edited_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface DailyInputResponse {
  local_date: string;
  input: DailyInput | null;
  tags: TagDayLink[];
}

// DailyInputUpsertBody — wire shape for PUT/PATCH. Tag arrays are
// rewritten in lockstep with the row, so passing [] clears that role's
// tags for the day.
export interface DailyInputUpsertBody {
  mood: number | null;
  drained_text: string;
  charged_text: string;
  gratitude_text: string;
  reflection_text: string;
  drained_tag_ids: string[];
  charged_tag_ids: string[];
}

export function getDailyInput(date?: string): Promise<DailyInputResponse> {
  const qs = date ? `?date=${encodeURIComponent(date)}` : "";
  return api(`/api/daily/inputs${qs}`);
}

// Upserts today's daily input. Server resolves "today" from the user's
// timezone + day_start_minutes — same convention as /api/entries.
export function saveDailyInput(body: DailyInputUpsertBody): Promise<
  DailyInputResponse | { deleted: boolean; local_date: string }
> {
  return api("/api/daily/inputs", { method: "PUT", body });
}

// Edits a past day's check-in. HistoryView calls this to amend a
// past-day mood after the day-start cutoff has rolled over.
export function updateDailyInputByDate(
  date: string,
  body: DailyInputUpsertBody,
): Promise<DailyInputResponse | { deleted: boolean; local_date: string }> {
  return api(`/api/daily/inputs/by-date/${encodeURIComponent(date)}`, {
    method: "PATCH",
    body,
  });
}

// Tag CRUD ----------------------------------------------------------------

export interface TagsListResponse {
  tags: Tag[];
}

// Lists active tags, optionally filtered by valence. The picker passes
// "positive" or "negative" to scope the dropdown to drainer-or-charger.
export function listTags(valence?: "positive" | "negative" | "neutral"): Promise<TagsListResponse> {
  const qs = valence ? `?valence=${valence}` : "";
  return api(`/api/tags${qs}`);
}

// Creates (or returns the existing matching) tag. Server upserts on
// normalized_label so re-clicking "add new" with the same label is
// idempotent.
export function createTag(label: string, valence: Tag["valence"]): Promise<Tag> {
  return api("/api/tags", { method: "POST", body: { label, valence } });
}

export function renameTag(id: string, label: string): Promise<Tag> {
  return api(`/api/tags/${id}`, { method: "PATCH", body: { label } });
}

export function archiveTag(id: string): Promise<void> {
  return api(`/api/tags/${id}`, { method: "DELETE" });
}

export function mergeTag(srcId: string, intoTagId: string): Promise<void> {
  return api(`/api/tags/${srcId}/merge`, {
    method: "POST",
    body: { into_tag_id: intoTagId },
  });
}
