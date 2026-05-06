import { useState } from "react";
import { Loader2 } from "lucide-react";

import { Textarea } from "@/components/ui/textarea";
import { StatusPill } from "@/components/ui/status-pill";
import { useDebouncedFlag } from "@/lib/useDebouncedFlag";
import { cn } from "@/lib/utils";

import { JournalEntry } from "./api";
import { useUpdateEntry } from "./hooks";

interface Props {
  entry: JournalEntry;
  prompt: string;
  localDate: string;
}

// Mirrors QuestionAnswer but PATCHes by entry id so the user can't backdate.
// Same optimistic-update story — cache is the feedback, no inline status
// text on every keystroke.
export function HistoryEntryEditor({ entry, prompt, localDate }: Props) {
  const serverBody = entry.body;
  const [lastServerBody, setLastServerBody] = useState(serverBody);
  const [draft, setDraft] = useState(serverBody);

  if (serverBody !== lastServerBody) {
    if (draft === lastServerBody) {
      setDraft(serverBody);
    }
    setLastServerBody(serverBody);
  }

  const update = useUpdateEntry(localDate);
  const dirty = draft !== serverBody;
  const showSpinner = useDebouncedFlag(update.isPending, 300);

  const handleBlur = () => {
    if (!dirty) return;
    update.mutate({ id: entry.id, body: draft });
  };

  return (
    <article className="space-y-2">
      <header className="flex items-baseline justify-between gap-3">
        <h3 className="border-l-2 border-accent/40 pl-3 font-serif text-base leading-snug">
          {prompt}
        </h3>
        <div className="flex items-center gap-2">
          {showSpinner ? (
            <Loader2
              className="h-3.5 w-3.5 animate-spin text-muted-foreground"
              aria-label="Saving"
            />
          ) : null}
          {dirty ? <StatusPill state="dirty" /> : null}
        </div>
      </header>
      <Textarea
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={handleBlur}
        rows={4}
        placeholder="Empty to delete this entry."
        className={cn(
          "border-transparent bg-transparent px-0 leading-prose text-body",
          "focus-visible:ring-0 focus-visible:ring-offset-0",
          "focus-visible:border-b-border focus-visible:border-b rounded-none",
        )}
      />
    </article>
  );
}
