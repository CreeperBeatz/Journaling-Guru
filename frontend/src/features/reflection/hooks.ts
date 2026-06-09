import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import type { ApiError } from "@/api/client";
import { ME_KEY } from "@/features/auth/useAuth";
import type { ChatSessionEnvelope } from "@/features/chat/api";
import { heatmapKey } from "@/features/journal/hooks";

import {
  PatchReflectionBody,
  ReflectionResponse,
  completeReflection,
  createOrResumeWeeklyChat,
  getReflectionChatByWeek,
  getThisWeekChat,
  getThisWeekReflection,
  patchReflection,
  patchReflectionByWeek,
  replayReflection,
  setMonthlyIntention,
  setMonthlyRatings,
  startReflection,
} from "./api";

export const REFLECTION_THIS_WEEK_KEY = ["reflection", "this-week"] as const;

// Weekly chat cache keys — separate from the daily chat keys
// (`chatSessionKey()` in features/chat) so envelopes don't collide.
export const weeklyChatKey = ["chat", "weekly", "this-week"] as const;
export const weeklyChatByWeekKey = (weekStart: string) =>
  ["chat", "weekly", weekStart] as const;

// useThisWeekReflection loads (without forcing) the cached reflection
// for the current week. WeeklyChat reads this to look up active-goal
// titles for the inline extend/complete cards.
export function useThisWeekReflection() {
  return useQuery<ReflectionResponse, Error>({
    queryKey: REFLECTION_THIS_WEEK_KEY,
    queryFn: getThisWeekReflection,
    staleTime: 60_000,
  });
}

// useStartReflection lazily creates the weekly_reflections row and seeds
// the FE cache so the wizard renders Card 1 immediately.
export function useStartReflection() {
  const qc = useQueryClient();
  return useMutation<ReflectionResponse, Error>({
    mutationFn: startReflection,
    onSuccess: (data) => {
      qc.setQueryData(REFLECTION_THIS_WEEK_KEY, data);
    },
  });
}

// usePatchReflection — partial wizard updates (surprise_text, step,
// goal note). Cache write is optimistic-ish: we set whatever the server
// returned. No optimistic update on fail; caller surfaces toast.
export function usePatchReflection() {
  const qc = useQueryClient();
  return useMutation<ReflectionResponse, Error, PatchReflectionBody>({
    mutationFn: patchReflection,
    onSuccess: (data) => {
      qc.setQueryData(REFLECTION_THIS_WEEK_KEY, data);
    },
  });
}

// usePatchReflectionByWeek — historical counterpart of usePatchReflection.
// Targets a past week's row; cache write goes to that week's by-week
// key so the History view re-renders with the new value.
export function usePatchReflectionByWeek(weekStart: string) {
  const qc = useQueryClient();
  const key = ["reflection", "by-week", weekStart] as const;
  return useMutation<ReflectionResponse, Error, PatchReflectionBody>({
    mutationFn: (body) => patchReflectionByWeek(weekStart, body),
    onSuccess: (data) => {
      qc.setQueryData(key, data);
    },
  });
}

// useCompleteReflection — flips completed_at. The wizard switches to
// the Done view on the cache update.
export function useCompleteReflection() {
  const qc = useQueryClient();
  return useMutation<ReflectionResponse, Error>({
    mutationFn: completeReflection,
    onSuccess: (data) => {
      qc.setQueryData(REFLECTION_THIS_WEEK_KEY, data);
      // Refresh the heatmap so the accent dot lands on the week_end cell
      // — the response carries the new completed_at but the heatmap is
      // a separate query keyed by date range.
      qc.invalidateQueries({ queryKey: heatmapKey() });
      // reflection_pending lives on /api/me — refetch so the Weekly nav
      // button hides right away on carry-over days.
      qc.invalidateQueries({ queryKey: ME_KEY });
    },
  });
}

// ----- Monthly reflection hooks -----

// useSetMonthlyIntention persists the month's intention (accepted or
// edited from the ProposeIntentionCard, or typed on the Summary tab).
// The response is the full ReflectionResponse — swap the cache.
export function useSetMonthlyIntention() {
  const qc = useQueryClient();
  return useMutation<ReflectionResponse, Error, string>({
    mutationFn: setMonthlyIntention,
    onSuccess: (data) => {
      qc.setQueryData(REFLECTION_THIS_WEEK_KEY, data);
    },
  });
}

// useSetMonthlyRatings persists the life check-in sliders.
export function useSetMonthlyRatings() {
  const qc = useQueryClient();
  return useMutation<ReflectionResponse, Error, Record<string, number>>({
    mutationFn: setMonthlyRatings,
    onSuccess: (data) => {
      qc.setQueryData(REFLECTION_THIS_WEEK_KEY, data);
    },
  });
}

// ----- Weekly reflection chat hooks -----

// useWeeklyChatSession is the read-side fetch — returns the existing
// weekly chat envelope or {session: null, ...} when none exists yet.
export function useWeeklyChatSession() {
  return useQuery<ChatSessionEnvelope, ApiError>({
    queryKey: weeklyChatKey,
    queryFn: getThisWeekChat,
    staleTime: 60_000,
  });
}

// useCreateOrResumeWeeklyChat lazily creates the weekly chat session
// (idempotent server-side) and seeds the cache.
export function useCreateOrResumeWeeklyChat() {
  const qc = useQueryClient();
  return useMutation<ChatSessionEnvelope, ApiError, void>({
    mutationFn: createOrResumeWeeklyChat,
    onSuccess: (env) => {
      qc.setQueryData<ChatSessionEnvelope>(weeklyChatKey, env);
    },
  });
}

// useWeeklyChatByWeek loads a past-week weekly chat transcript for the
// history view. Cached immutably (past weeks don't change).
export function useWeeklyChatByWeek(weekStart: string | null) {
  return useQuery<ChatSessionEnvelope, ApiError>({
    queryKey: weeklyChatByWeekKey(weekStart ?? "none"),
    queryFn: () => getReflectionChatByWeek(weekStart as string),
    enabled: !!weekStart,
    staleTime: Infinity,
  });
}

// useReplayReflection — clears completed_at and rewinds the wizard to
// step 1. Preserves surprise_text and goal_notes so the user can
// re-walk what they already wrote. Heatmap is invalidated since the
// accent dot reflects completed_at and we just cleared it.
export function useReplayReflection() {
  const qc = useQueryClient();
  return useMutation<ReflectionResponse, Error>({
    mutationFn: replayReflection,
    onSuccess: (data) => {
      qc.setQueryData(REFLECTION_THIS_WEEK_KEY, data);
      qc.invalidateQueries({ queryKey: heatmapKey() });
      // Replay rewinds the chat phase from finalized → exploring server-
      // side; refetch the weekly chat envelope so the FE sees the new
      // phase and lifts its read-only lock.
      qc.invalidateQueries({ queryKey: weeklyChatKey });
      // Replay deletes the row server-side → the week is pending again;
      // refetch /api/me so the Weekly nav button reappears on carry-over
      // days.
      qc.invalidateQueries({ queryKey: ME_KEY });
    },
  });
}
