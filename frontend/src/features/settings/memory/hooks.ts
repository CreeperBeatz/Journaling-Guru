import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { ApiError } from "@/api/client";
import { toast } from "@/components/ui/sonner";

import {
  MemoriesResponse,
  Memory,
  MemoryWriteBody,
  createMemory,
  deleteMemory,
  listMemories,
  updateMemory,
} from "./api";

export const memoriesKey = ["memories"] as const;

export function useMemories() {
  return useQuery<MemoriesResponse, ApiError>({
    queryKey: memoriesKey,
    queryFn: listMemories,
    staleTime: 30_000,
  });
}

// Simple invalidation rather than optimistic writes — the list is small
// and memory edits aren't latency-sensitive (unlike goal check-ins).
export function useCreateMemory() {
  const qc = useQueryClient();
  return useMutation<Memory, ApiError, MemoryWriteBody>({
    mutationFn: createMemory,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: memoriesKey });
      toast.success("Memory added");
    },
    onError: (err) => toast.error("Couldn't add memory", { description: err.message }),
  });
}

export function useUpdateMemory() {
  const qc = useQueryClient();
  return useMutation<Memory, ApiError, { id: string; body: MemoryWriteBody }>({
    mutationFn: ({ id, body }) => updateMemory(id, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: memoriesKey });
      toast.success("Memory updated");
    },
    onError: (err) => toast.error("Couldn't update memory", { description: err.message }),
  });
}

export function useDeleteMemory() {
  const qc = useQueryClient();
  return useMutation<void, ApiError, string>({
    mutationFn: deleteMemory,
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: memoriesKey });
      toast.success("Memory removed");
    },
    onError: (err) => toast.error("Couldn't remove memory", { description: err.message }),
  });
}
