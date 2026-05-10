import { useMutation, useQueryClient } from "@tanstack/react-query";

import {
  PatchReflectionBody,
  ReflectionResponse,
  completeReflection,
  patchReflection,
  startReflection,
} from "./api";

export const REFLECTION_THIS_WEEK_KEY = ["reflection", "this-week"] as const;

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

// useCompleteReflection — flips completed_at. The wizard switches to
// the Done view on the cache update.
export function useCompleteReflection() {
  const qc = useQueryClient();
  return useMutation<ReflectionResponse, Error>({
    mutationFn: completeReflection,
    onSuccess: (data) => {
      qc.setQueryData(REFLECTION_THIS_WEEK_KEY, data);
    },
  });
}
