import { useState } from "react";
import { motion, useReducedMotion } from "motion/react";
import { Archive, ChevronDown, ChevronUp, GripVertical, Pencil } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";

import { Question } from "./api";
import {
  useArchiveQuestion,
  useCreateQuestion,
  useQuestions,
  useReorderQuestions,
  useUpdateQuestion,
} from "./hooks";

interface RowProps {
  question: Question;
  isFirst: boolean;
  isLast: boolean;
  onMove: (delta: -1 | 1) => void;
}

function QuestionRow({ question, isFirst, isLast, onMove }: RowProps) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(question.prompt);
  const update = useUpdateQuestion();
  const archive = useArchiveQuestion();
  const reduce = useReducedMotion();

  const save = () => {
    const trimmed = draft.trim();
    if (!trimmed || trimmed === question.prompt) {
      setEditing(false);
      setDraft(question.prompt);
      return;
    }
    update.mutate(
      { id: question.id, prompt: trimmed },
      { onSuccess: () => setEditing(false) },
    );
  };

  return (
    <motion.li
      layout={reduce ? false : "position"}
      transition={{ type: "spring", stiffness: 500, damping: 38 }}
      className="flex flex-wrap items-center gap-2 rounded-lg border border-border bg-card p-3 shadow-xs transition-shadow hover:shadow-sm"
    >
      <GripVertical
        className="h-4 w-4 shrink-0 text-muted-foreground/50"
        aria-hidden="true"
      />
      <div className="flex flex-col gap-0.5">
        <Button
          type="button"
          variant="ghost"
          size="icon"
          aria-label="Move up"
          onClick={() => onMove(-1)}
          disabled={isFirst}
          className="h-6 w-6"
        >
          <ChevronUp className="h-3.5 w-3.5" />
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          aria-label="Move down"
          onClick={() => onMove(1)}
          disabled={isLast}
          className="h-6 w-6"
        >
          <ChevronDown className="h-3.5 w-3.5" />
        </Button>
      </div>

      <div className="flex-1 min-w-[16rem]">
        {editing ? (
          <Input
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") save();
              if (e.key === "Escape") {
                setEditing(false);
                setDraft(question.prompt);
              }
            }}
            autoFocus
            maxLength={500}
          />
        ) : (
          <p className="text-sm leading-snug">{question.prompt}</p>
        )}
      </div>

      <div className="flex gap-1">
        {editing ? (
          <>
            <Button size="sm" onClick={save} disabled={update.isPending}>
              {update.isPending ? "Saving…" : "Save"}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => {
                setEditing(false);
                setDraft(question.prompt);
              }}
            >
              Cancel
            </Button>
          </>
        ) : (
          <>
            <Button
              size="icon"
              variant="ghost"
              onClick={() => setEditing(true)}
              aria-label="Edit question"
              title="Edit"
              className="h-9 w-9"
            >
              <Pencil className="h-4 w-4" />
            </Button>
            <AlertDialog>
              <AlertDialogTrigger asChild>
                <Button
                  size="icon"
                  variant="ghost"
                  aria-label="Archive question"
                  title="Archive"
                  className="h-9 w-9 text-muted-foreground hover:text-destructive"
                  disabled={archive.isPending}
                >
                  <Archive className="h-4 w-4" />
                </Button>
              </AlertDialogTrigger>
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>Archive this question?</AlertDialogTitle>
                  <AlertDialogDescription>
                    Past answers stay visible in history. The prompt stops
                    appearing on Today.
                  </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel>Cancel</AlertDialogCancel>
                  <AlertDialogAction
                    onClick={() => archive.mutate(question.id)}
                    className={cn(buttonVariants({ variant: "destructive" }))}
                  >
                    Archive
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
          </>
        )}
      </div>
    </motion.li>
  );
}

export function QuestionEditor() {
  const questions = useQuestions();
  const create = useCreateQuestion();
  const reorder = useReorderQuestions();
  const [draft, setDraft] = useState("");

  const move = (id: string, delta: -1 | 1) => {
    const list = (questions.data ?? []).map((q) => q.id);
    const idx = list.indexOf(id);
    const swap = idx + delta;
    if (idx < 0 || swap < 0 || swap >= list.length) return;
    [list[idx], list[swap]] = [list[swap], list[idx]];
    reorder.mutate(list);
  };

  const submit = () => {
    const trimmed = draft.trim();
    if (!trimmed) return;
    create.mutate(trimmed, {
      onSuccess: () => setDraft(""),
    });
  };

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <p className="text-xs uppercase tracking-wider text-muted-foreground">Questions</p>
        <h1 className="font-serif text-h1">Manage prompts</h1>
        <p className="text-sm text-muted-foreground">
          Reorder and archive prompts. Archived questions keep their history but
          stop showing on Today.
        </p>
      </header>

      <Card>
        <CardHeader>
          <CardTitle className="font-serif">Add a new question</CardTitle>
          <CardDescription>Up to 500 characters. Appears at the bottom of Today.</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3 sm:flex-row">
          <Input
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") submit();
            }}
            placeholder="What's a question you want to ask yourself daily?"
            maxLength={500}
          />
          <Button onClick={submit} disabled={create.isPending || draft.trim() === ""}>
            {create.isPending ? "Adding…" : "Add"}
          </Button>
        </CardContent>
      </Card>

      {questions.isPending ? (
        <p className="text-sm text-muted-foreground">Loading…</p>
      ) : questions.isError ? (
        <p className="text-sm text-destructive">Couldn't load questions.</p>
      ) : (questions.data ?? []).length === 0 ? (
        <p className="text-sm text-muted-foreground">No questions yet.</p>
      ) : (
        <ul className="space-y-2">
          {(questions.data ?? []).map((q, idx, all) => (
            <QuestionRow
              key={q.id}
              question={q}
              isFirst={idx === 0}
              isLast={idx === all.length - 1}
              onMove={(delta) => move(q.id, delta)}
            />
          ))}
        </ul>
      )}
    </div>
  );
}
