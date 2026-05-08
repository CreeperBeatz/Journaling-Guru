import { api } from "@/api/client";
import type { User } from "@/features/auth/api";

export interface UpdateMePatch {
  display_name?: string;
  timezone?: string;
  reminder_time?: string;
  reminder_enabled?: boolean;
  day_start_minutes?: number;
  reflection_weekday?: number; // 0=Sun..6=Sat
}

export function updateMe(patch: UpdateMePatch): Promise<User> {
  return api<User>("/api/me", { method: "PATCH", body: patch });
}
