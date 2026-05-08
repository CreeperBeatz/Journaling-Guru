import { useMemo, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus, X } from "lucide-react";

import { Input } from "@/components/ui/input";
import { ApiError } from "@/api/client";
import { toast } from "@/components/ui/sonner";
import { cn } from "@/lib/utils";

import { Tag, createTag } from "./api";
import { tagsKey, useTags } from "./hooks";

interface Props {
  // Side this picker is filling. Drives the tag query filter and the
  // valence assigned to newly-created tags.
  valence: "positive" | "negative";
  // Currently-selected tag IDs.
  selectedIds: string[];
  // Callback when the selection changes. Called with the next list of
  // selected IDs; parent owns the source of truth.
  onChange: (ids: string[]) => void;
  // Optional placeholder for the inline "add" input.
  placeholder?: string;
  // Disabled state — locks the picker (e.g. when a save is in flight
  // and the parent doesn't want concurrent mutation).
  disabled?: boolean;
}

// TagPicker — multi-select tag chooser for one valence side. Reused for
// drainers (negative) and chargers (positive) on the Manual tab.
//
// UX shape:
//   - Selected pills row at the top, each with an × to deselect.
//   - "+ Add" inline affordance at the right of the pills row that
//     opens a small input + suggestion list. Typing filters existing
//     active tags by prefix; Enter on a suggestion selects it; Enter
//     on free text creates (or upserts) a new tag and selects it.
//
// Picker queries `useTags(valence)` so it shares the cache with the
// daily-input optimistic-update lookup map.
export function TagPicker({
  valence,
  selectedIds,
  onChange,
  placeholder,
  disabled,
}: Props) {
  const tagsQuery = useTags(valence);
  const allTags = tagsQuery.data?.tags ?? [];
  const selectedSet = useMemo(() => new Set(selectedIds), [selectedIds]);

  const selected = allTags.filter((t) => selectedSet.has(t.id));
  // Fallback for IDs not yet in the cache (e.g. just-created server-side
  // and the list hasn't refetched). Render as a placeholder pill so the
  // user sees feedback.
  const missingIds = selectedIds.filter((id) => !allTags.some((t) => t.id === id));

  const remove = (id: string) => {
    onChange(selectedIds.filter((x) => x !== id));
  };
  const add = (id: string) => {
    if (selectedSet.has(id)) return;
    onChange([...selectedIds, id]);
  };

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-1.5">
        {selected.map((t) => (
          <TagPill key={t.id} label={t.label} onRemove={() => remove(t.id)} disabled={disabled} />
        ))}
        {missingIds.map((id) => (
          <TagPill key={id} label="…" onRemove={() => remove(id)} disabled={disabled} muted />
        ))}
        <AddTagControl
          valence={valence}
          existing={allTags}
          selectedIds={selectedIds}
          onPick={add}
          placeholder={placeholder}
          disabled={disabled}
        />
      </div>
    </div>
  );
}

function TagPill({
  label,
  onRemove,
  disabled,
  muted,
}: {
  label: string;
  onRemove: () => void;
  disabled?: boolean;
  muted?: boolean;
}) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs",
        muted
          ? "border-dashed border-border text-muted-foreground"
          : "border-border bg-secondary/40 text-foreground",
      )}
    >
      {label}
      <button
        type="button"
        onClick={onRemove}
        disabled={disabled}
        aria-label={`Remove ${label}`}
        className="rounded p-0.5 text-muted-foreground hover:text-foreground disabled:opacity-50"
      >
        <X className="h-3 w-3" />
      </button>
    </span>
  );
}

function AddTagControl({
  valence,
  existing,
  selectedIds,
  onPick,
  placeholder,
  disabled,
}: {
  valence: Tag["valence"];
  existing: Tag[];
  selectedIds: string[];
  onPick: (id: string) => void;
  placeholder?: string;
  disabled?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState("");
  const inputRef = useRef<HTMLInputElement | null>(null);
  const qc = useQueryClient();

  const create = useMutation<Tag, ApiError, { label: string }>({
    mutationFn: ({ label }) => createTag(label, valence),
    onSuccess: (tag) => {
      qc.invalidateQueries({ queryKey: tagsKey() });
      qc.invalidateQueries({ queryKey: tagsKey(valence) });
      onPick(tag.id);
      setText("");
    },
    onError: (err) => toast.error("Couldn't add tag", { description: err.message }),
  });

  const lower = text.trim().toLowerCase();
  const suggestions = useMemo(() => {
    if (!lower) {
      return existing.filter((t) => !selectedIds.includes(t.id)).slice(0, 6);
    }
    return existing
      .filter(
        (t) => !selectedIds.includes(t.id) && t.label.toLowerCase().includes(lower),
      )
      .slice(0, 6);
  }, [existing, lower, selectedIds]);

  const exactMatch = existing.find(
    (t) => t.label.toLowerCase() === lower && !selectedIds.includes(t.id),
  );

  const handleEnter = () => {
    if (!lower) return;
    if (exactMatch) {
      onPick(exactMatch.id);
      setText("");
      return;
    }
    create.mutate({ label: text.trim() });
  };

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => {
          setOpen(true);
          queueMicrotask(() => inputRef.current?.focus());
        }}
        disabled={disabled}
        className={cn(
          "inline-flex items-center gap-1 rounded-full border border-dashed border-border",
          "px-2 py-0.5 text-xs text-muted-foreground transition-colors",
          "hover:border-border hover:text-foreground disabled:opacity-50",
        )}
      >
        <Plus className="h-3 w-3" />
        Add
      </button>
    );
  }

  return (
    <div className="relative inline-block">
      <Input
        ref={inputRef}
        value={text}
        onChange={(e) => setText(e.target.value)}
        onBlur={() => {
          // Defer so suggestion clicks land first.
          setTimeout(() => setOpen(false), 120);
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            handleEnter();
          } else if (e.key === "Escape") {
            setOpen(false);
            setText("");
          }
        }}
        placeholder={placeholder ?? "tag…"}
        disabled={disabled || create.isPending}
        className="h-7 w-40 rounded-full px-3 text-xs"
      />
      {(suggestions.length > 0 || (lower && !exactMatch)) ? (
        <div
          className={cn(
            "absolute left-0 top-full z-20 mt-1 min-w-[180px] rounded-md border bg-popover p-1 shadow-md",
          )}
          // Stop blur-then-close from killing the click.
          onMouseDown={(e) => e.preventDefault()}
        >
          {suggestions.map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => {
                onPick(t.id);
                setText("");
                setOpen(false);
              }}
              className="block w-full truncate rounded px-2 py-1 text-left text-xs hover:bg-secondary"
            >
              {t.label}
            </button>
          ))}
          {lower && !exactMatch ? (
            <button
              type="button"
              onClick={handleEnter}
              disabled={create.isPending}
              className={cn(
                "block w-full truncate rounded px-2 py-1 text-left text-xs",
                "text-accent hover:bg-secondary",
              )}
            >
              + Create &ldquo;{text.trim()}&rdquo;
            </button>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
