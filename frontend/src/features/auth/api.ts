// Auth-shaped wrappers around the shared fetch client. Every call sends
// `credentials: 'include'` (set by the api wrapper) so the session cookie
// rides on the request — which is what makes /api/me work after verify.

import { api } from "@/api/client";

export interface User {
  id: string;
  email: string;
  email_verified: boolean;
  display_name?: string | null;
  timezone: string;
  reminder_time: string;
  reminder_enabled: boolean;
  day_start_minutes: number;
  reflection_weekday: number; // 0=Sun..6=Sat — day for the weekly reflection flow

  created_at: string;
  updated_at: string;
}

export function requestMagicLink(email: string): Promise<{ ok: boolean }> {
  return api("/api/auth/magic-link", {
    method: "POST",
    body: { email },
  });
}

export function verifyMagicLink(token: string): Promise<{ user_id: string }> {
  return api("/api/auth/verify", {
    method: "POST",
    body: { token },
  });
}

export function logout(): Promise<{ ok: boolean }> {
  return api("/api/auth/logout", { method: "POST" });
}

export function fetchMe(): Promise<User> {
  return api<User>("/api/me");
}

export function deleteAccount(): Promise<{ ok: boolean }> {
  return api("/api/account", { method: "DELETE" });
}
