import { api } from "@/api/client";

// Memory — one durable fact about the user's life, accumulated by the
// nightly reconciliation pass over the day's journal record and woven
// into chat sessions. `pinned` means user-edited or user-created: the
// automatic pass will never change or remove it (any edit here pins the
// row server-side).
export interface Memory {
  id: string;
  category: MemoryCategory;
  content: string;
  status: "active";
  pinned: boolean;
  source: "extraction" | "user";
  source_local_date?: string; // YYYY-MM-DD; absent for user-created
  created_at: string;
  updated_at: string;
}

export const MEMORY_CATEGORIES = [
  "identity",
  "relationships",
  "work",
  "health",
  "preferences",
  "goals",
  "routines",
  "other",
] as const;

export type MemoryCategory = (typeof MEMORY_CATEGORIES)[number];

export const MEMORY_CATEGORY_LABELS: Record<MemoryCategory, string> = {
  identity: "Identity",
  relationships: "Relationships",
  work: "Work",
  health: "Health",
  preferences: "Preferences",
  goals: "Goals & aspirations",
  routines: "Routines",
  other: "Other",
};

export const MAX_MEMORY_CONTENT_LEN = 500;

export interface MemoriesResponse {
  memories: Memory[];
}

export function listMemories(): Promise<MemoriesResponse> {
  return api("/api/memories");
}

export interface MemoryWriteBody {
  category: MemoryCategory;
  content: string;
}

export function createMemory(body: MemoryWriteBody): Promise<Memory> {
  return api("/api/memories", { method: "POST", body });
}

export function updateMemory(id: string, body: MemoryWriteBody): Promise<Memory> {
  return api(`/api/memories/${id}`, { method: "PATCH", body });
}

export function deleteMemory(id: string): Promise<void> {
  return api(`/api/memories/${id}`, { method: "DELETE" });
}
