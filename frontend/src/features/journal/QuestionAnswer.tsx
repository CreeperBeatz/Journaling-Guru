import { useState } from "react";
import { Loader2 } from "lucide-react";

import { Textarea } from "@/components/ui/textarea";
import { StatusPill, type StatusState } from "@/components/ui/status-pill";
import { useDebouncedFlag } from "@/lib/useDebouncedFlag";
import { cn } from "@/lib/utils";

import { JournalEntry, Question } from "./api";
import { useSaveEntry } from "./hooks";

interface Props {
  question: Question;
  entry: JournalEntry | undefined;
}

// QuestionAnswer is one prompt + its editable answer for "today". The
// optimistic save in useSaveEntry IS the feedback — the textarea persists
// the new body into cache the moment we mutate. We no longer render
// inline "Saving…/Saved/Unsaved" text on every keystroke; instead the
// pill shows `dirty` only while the user has unflushed edits, and a
// debounced spinner appears only if a save genuinely takes >300ms.
export function QuestionAnswer({ question, entry }: Props) {
  const serverBody = entry?.body ?? "";
  const [lastServerBody, setLastServerBody] = useState(serverBody);
  const [draft, setDraft] = useState(serverBody);

  if (serverBody !== lastServerBody) {
    if (draft === lastServerBody) {
      setDraft(serverBody);
    }
    setLastServerBody(serverBody);
  }

  const save = useSaveEntry();
  const dirty = draft !== serverBody;
  const showSpinner = useDebouncedFlag(save.isPending, 300);

  const handleBlur = () => {
    if (!dirty) return;
    save.mutate({ questionId: question.id, body: draft });
  };

  const state: StatusState = dirty ? "dirty" : "idle";

  return (
    <section className="group relative space-y-3 rounded-xl border border-border bg-card p-5 shadow-sm">
      <header className="flex items-start justify-between gap-3">
        <h3 className="border-l-2 border-accent/50 pl-3 font-serif text-h3 leading-snug">
          {question.prompt}
        </h3>
        <div className="flex items-center gap-2 pt-0.5">
          {showSpinner ? (
            <Loader2
              className="h-3.5 w-3.5 animate-spin text-muted-foreground"
              aria-label="Saving"
            />
          ) : null}
          {dirty ? <StatusPill state={state} /> : null}
        </div>
      </header>
      <Textarea
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={handleBlur}
        placeholder="Write whatever comes to mind…"
        rows={4}
        className={cn(
          "border-transparent bg-transparent px-0 leading-prose text-body",
          "focus-visible:ring-0 focus-visible:ring-offset-0",
          "focus-visible:border-b-border focus-visible:border-b rounded-none",
        )}
      />
    </section>
  );
}
