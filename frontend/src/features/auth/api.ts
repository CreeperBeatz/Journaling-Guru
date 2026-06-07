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
  timezone_auto: boolean;
  reminder_time: string;
  reminder_enabled: boolean;
  day_start_minutes: number;
  reflection_weekday: number; // 0=Sun..6=Sat — day for the weekly reflection flow
  // local_weekday is server-computed from timezone + day_start_minutes —
  // gating the weekly-reflection UI against it (rather than `new Date()`)
  // respects the late-night cutoff so a 1am session still reads as
  // yesterday. Optional because a misconfigured tz falls back to omitting it.
  local_weekday?: number; // 0=Sun..6=Sat
  // reflection_pending is server-computed: the current canonical
  // reflection week (ending on the most recent reflection_weekday) had
  // logged activity but no completed weekly reflection yet. Keeps the
  // Weekly nav button visible on carry-over days after a missed
  // reflection day. Optional — omitted when it can't be derived.
  reflection_pending?: boolean;
  // onboarded_at is null until the first-run walkthrough at /onboarding
  // finishes (or is skipped). AppShell uses null to gate the redirect.
  onboarded_at: string | null;

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

export function fetchMe(tzHint?: string | null): Promise<User> {
  const path = tzHint ? `/api/me?tz=${encodeURIComponent(tzHint)}` : "/api/me";
  return api<User>(path);
}

export function deleteAccount(): Promise<{ ok: boolean }> {
  return api("/api/account", { method: "DELETE" });
}
