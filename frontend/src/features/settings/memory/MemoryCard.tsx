import { useMemo, useState } from "react";
import { Lock, Pencil, Plus, Trash2, X } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { Skeleton } from "@/components/ui/skeleton";

import {
  MAX_MEMORY_CONTENT_LEN,
  MEMORY_CATEGORIES,
  MEMORY_CATEGORY_LABELS,
  Memory,
  MemoryCategory,
} from "./api";
import { useCreateMemory, useDeleteMemory, useMemories, useUpdateMemory } from "./hooks";

// MemoryCard — the "What the companion knows about you" management
// surface. Lists active memories grouped by category; rows are editable
// and deletable. Any edit or manual add pins the row server-side, which
// locks it against the automatic nightly pass — surfaced here as a small
// lock badge.
export function MemoryCard() {
  const memories = useMemories();
  const createM = useCreateMemory();
  const [adding, setAdding] = useState(false);

  const grouped = useMemo(() => {
    const rows = memories.data?.memories ?? [];
    return MEMORY_CATEGORIES.map((cat) => ({
      category: cat,
      items: rows.filter((m) => m.category === cat),
    })).filter((g) => g.items.length > 0);
  }, [memories.data]);

  const empty = !memories.isPending && grouped.length === 0;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-serif">Memory</CardTitle>
        <CardDescription>
          Things your companion has learned about your life from your
          journaling — and uses to keep conversations grounded. Edit or
          remove anything; edited memories are locked and won't be changed
          by the automatic nightly pass.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        {memories.isPending ? (
          <div className="space-y-2">
            <Skeleton className="h-4 w-24" />
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-full" />
          </div>
        ) : empty ? (
          <p className="text-sm text-muted-foreground">
            Nothing yet. Memories accumulate as you journal — durable facts
            (a new job, a person who matters, a routine) get noticed at the
            end of each day. You can also add one yourself below.
          </p>
        ) : (
          grouped.map((g) => (
            <section key={g.category} className="space-y-2">
              <h3 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                {MEMORY_CATEGORY_LABELS[g.category]}
              </h3>
              <ul className="space-y-1.5">
                {g.items.map((m) => (
                  <MemoryRow key={m.id} memory={m} />
                ))}
              </ul>
            </section>
          ))
        )}

        {adding ? (
          <MemoryForm
            initialCategory="other"
            initialContent=""
            submitLabel="Add memory"
            pending={createM.isPending}
            onCancel={() => setAdding(false)}
            onSubmit={(category, content) =>
              createM.mutate(
                { category, content },
                { onSuccess: () => setAdding(false) },
              )
            }
          />
        ) : (
          <Button variant="secondary" size="sm" onClick={() => setAdding(true)}>
            <Plus className="mr-1.5 h-4 w-4" aria-hidden />
            Add memory
          </Button>
        )}
      </CardContent>
    </Card>
  );
}

function MemoryRow({ memory }: { memory: Memory }) {
  const updateM = useUpdateMemory();
  const deleteM = useDeleteMemory();
  const [editing, setEditing] = useState(false);

  if (editing) {
    return (
      <li>
        <MemoryForm
          initialCategory={memory.category}
          initialContent={memory.content}
          submitLabel="Save"
          pending={updateM.isPending}
          onCancel={() => setEditing(false)}
          onSubmit={(category, content) =>
            updateM.mutate(
              { id: memory.id, body: { category, content } },
              { onSuccess: () => setEditing(false) },
            )
          }
        />
      </li>
    );
  }

  return (
    <li className="group flex items-start gap-2 rounded-md border border-border/60 bg-muted/30 px-3 py-2">
      <p className="min-w-0 flex-1 text-sm leading-snug">{memory.content}</p>
      {memory.pinned ? (
        <span
          className="mt-0.5 inline-flex shrink-0 items-center gap-1 rounded-full bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground"
          title="Locked — the automatic pass won't change this"
        >
          <Lock className="h-3 w-3" aria-hidden />
          locked
        </span>
      ) : null}
      <div className="flex shrink-0 items-center gap-0.5">
        <Button
          variant="ghost"
          size="icon"
          className="h-7 w-7 text-muted-foreground hover:text-foreground"
          aria-label="Edit memory"
          onClick={() => setEditing(true)}
        >
          <Pencil className="h-3.5 w-3.5" aria-hidden />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          className="h-7 w-7 text-muted-foreground hover:text-destructive"
          aria-label="Delete memory"
          disabled={deleteM.isPending}
          onClick={() => deleteM.mutate(memory.id)}
        >
          <Trash2 className="h-3.5 w-3.5" aria-hidden />
        </Button>
      </div>
    </li>
  );
}

function MemoryForm({
  initialCategory,
  initialContent,
  submitLabel,
  pending,
  onSubmit,
  onCancel,
}: {
  initialCategory: MemoryCategory;
  initialContent: string;
  submitLabel: string;
  pending: boolean;
  onSubmit: (category: MemoryCategory, content: string) => void;
  onCancel: () => void;
}) {
  const [category, setCategory] = useState<MemoryCategory>(initialCategory);
  const [content, setContent] = useState(initialContent);
  const trimmed = content.trim();
  const valid = trimmed.length > 0 && trimmed.length <= MAX_MEMORY_CONTENT_LEN;

  return (
    <div className="space-y-2 rounded-md border border-border bg-muted/30 p-3">
      <Select value={category} onValueChange={(v) => setCategory(v as MemoryCategory)}>
        <SelectTrigger className="h-8 w-full sm:w-56">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {MEMORY_CATEGORIES.map((c) => (
            <SelectItem key={c} value={c}>
              {MEMORY_CATEGORY_LABELS[c]}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Textarea
        value={content}
        onChange={(e) => setContent(e.target.value)}
        placeholder="One fact, one sentence — e.g. “My sister Mara lives nearby.”"
        rows={2}
        maxLength={MAX_MEMORY_CONTENT_LEN}
        autoFocus
      />
      <div className="flex items-center justify-end gap-2">
        <Button variant="ghost" size="sm" onClick={onCancel}>
          <X className="mr-1 h-3.5 w-3.5" aria-hidden />
          Cancel
        </Button>
        <Button
          size="sm"
          disabled={!valid || pending}
          onClick={() => onSubmit(category, trimmed)}
        >
          {pending ? "Saving…" : submitLabel}
        </Button>
      </div>
    </div>
  );
}
