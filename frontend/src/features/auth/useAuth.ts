import { useQuery, useQueryClient } from "@tanstack/react-query";

import { ApiError } from "@/api/client";
import { detectBrowserTimezone } from "@/lib/timezone";

import { fetchMe, type User } from "./api";

export const ME_KEY = ["me"] as const;

// useMe loads the signed-in user (or returns null on 401). Refetches on
// mount so an expired cookie surfaces as a redirect rather than a stale
// authenticated UI.
export function useMe() {
  return useQuery<User | null, ApiError>({
    queryKey: ME_KEY,
    queryFn: async () => {
      try {
        // Pass the browser-detected IANA zone as a hint. The server
        // silently auto-syncs users.timezone for users in auto mode and
        // ignores the hint for users with an explicit override.
        return await fetchMe(detectBrowserTimezone());
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) {
          return null;
        }
        throw err;
      }
    },
    staleTime: 60_000,
  });
}

// Useful after login/logout/delete to force a full re-evaluation of the
// authenticated UI without page reload.
export function useInvalidateMe() {
  const qc = useQueryClient();
  return () => qc.invalidateQueries({ queryKey: ME_KEY });
}
