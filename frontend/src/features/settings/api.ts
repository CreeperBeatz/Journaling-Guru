import { api } from "@/api/client";
import type { User } from "@/features/auth/api";

export interface UpdateMePatch {
  display_name?: string;
  timezone?: string;
  timezone_auto?: boolean;
  reminder_time?: string;
  reminder_enabled?: boolean;
  day_start_minutes?: number;
  reflection_weekday?: number; // 0=Sun..6=Sat
  // mark_onboarded=true stamps users.onboarded_at server-side. Used by the
  // /onboarding flow's final step to flip the gate; the actual timestamp
  // can't be set from the client.
  mark_onboarded?: boolean;
}

export function updateMe(patch: UpdateMePatch): Promise<User> {
  return api<User>("/api/me", { method: "PATCH", body: patch });
}
