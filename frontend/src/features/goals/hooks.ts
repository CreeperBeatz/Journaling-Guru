import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { ApiError } from "@/api/client";
import { toast } from "@/components/ui/sonner";

import {
  ActiveGoalsResponse,
  AllGoalsResponse,
  CreateGoalBody,
  Goal,
  abandonGoal,
  checkInGoal,
  completeGoal,
  createGoal,
  listActiveGoals,
  listAllGoals,
} from "./api";

export const goalsActiveKey = ["goals", "active"] as const;
export const goalsAllKey = ["goals", "all"] as const;

export function useActiveGoals() {
  return useQuery<ActiveGoalsResponse, ApiError>({
    queryKey: goalsActiveKey,
    queryFn: listActiveGoals,
    staleTime: 30_000,
  });
}

export function useAllGoals() {
  return useQuery<AllGoalsResponse, ApiError>({
    queryKey: goalsAllKey,
    queryFn: listAllGoals,
    staleTime: 30_000,
  });
}

export function useCreateGoal() {
  const qc = useQueryClient();
  return useMutation<Goal, ApiError, CreateGoalBody>({
    mutationFn: createGoal,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["goals"] });
      toast.success("Goal created");
    },
    onError: (err) => toast.error("Couldn't create goal", { description: err.message }),
  });
}

// Optimistic check-in: write the new value into the active-goals cache,
// roll back on failure. Avoids a flash of "unanswered" between click
// and server response.
export function useCheckInGoal() {
  const qc = useQueryClient();
  return useMutation<
    void,
    ApiError,
    { goalId: string; value: boolean },
    { prev?: ActiveGoalsResponse }
  >({
    mutationFn: async ({ goalId, value }) => {
      await checkInGoal(goalId, value);
    },
    onMutate: async ({ goalId, value }) => {
      await qc.cancelQueries({ queryKey: goalsActiveKey });
      const prev = qc.getQueryData<ActiveGoalsResponse>(goalsActiveKey);
      if (prev) {
        qc.setQueryData<ActiveGoalsResponse>(goalsActiveKey, {
          ...prev,
          todays_check_ins: { ...prev.todays_check_ins, [goalId]: value },
        });
      }
      return { prev };
    },
    onError: (err, _vars, ctx) => {
      if (ctx?.prev) qc.setQueryData(goalsActiveKey, ctx.prev);
      toast.error("Couldn't save check-in", { description: err.message });
    },
    onSettled: () => qc.invalidateQueries({ queryKey: goalsActiveKey }),
  });
}

export function useCompleteGoal() {
  const qc = useQueryClient();
  return useMutation<
    Goal,
    ApiError,
    { id: string; outcome: "kept" | "dropped" | "inconclusive"; conclusionText: string }
  >({
    mutationFn: ({ id, outcome, conclusionText }) =>
      completeGoal(id, outcome, conclusionText),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["goals"] }),
    onError: (err) => toast.error("Couldn't complete goal", { description: err.message }),
  });
}

export function useAbandonGoal() {
  const qc = useQueryClient();
  return useMutation<
    Goal,
    ApiError,
    { id: string; conclusionText: string }
  >({
    mutationFn: ({ id, conclusionText }) => abandonGoal(id, conclusionText),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["goals"] }),
    onError: (err) => toast.error("Couldn't abandon goal", { description: err.message }),
  });
}
