import { Link } from "react-router-dom";
import { useState } from "react";

import { useQuestions } from "@/features/journal/hooks";
import type { Question } from "@/features/journal/api";
import { cn } from "@/lib/utils";

import { useEntriesByQuestion } from "./hooks";

function formatHumanDate(yyyymmdd: string): string {
  const [y, m, d] = yyyymmdd.split("-").map(Number);
  if (!y || !m || !d) return yyyymmdd;
  const date = new Date(Date.UTC(y, m - 1, d));
  return date.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
    timeZone: "UTC",
  });
}

/**
 * Two-column layout (md+): left rail of questions; right vertical
 * timeline of every answer to the selected question. Read-only; tap a
 * date to jump to /history/:date. The place to notice drift across
 * weeks.
 */
export function ByQuestionTimeline() {
  const questions = useQuestions();
  const [selectedId, setSelectedId] = useState<string | null>(null);

  if (questions.isPending) {
    return <p className="text-sm text-muted-foreground">Loading…</p>;
  }
  if (questions.isError) {
    return (
      <p className="text-sm text-destructive">Couldn&apos;t load questions.</p>
    );
  }
  const list = questions.data ?? [];
  if (list.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        No questions yet. Add some in Settings → Questions.
      </p>
    );
  }
  const selected =
    selectedId ?? list[0]?.id ?? null;

  return (
    <div className="grid grid-cols-1 gap-6 md:grid-cols-[16rem,1fr]">
      <nav className="space-y-1" aria-label="Questions">
        {list.map((q) => (
          <QuestionRailRow
            key={q.id}
            question={q}
            active={q.id === selected}
            onSelect={() => setSelectedId(q.id)}
          />
        ))}
      </nav>
      <div className="min-h-[8rem]">
        {selected ? <Timeline questionId={selected} /> : null}
      </div>
    </div>
  );
}

function QuestionRailRow({
  question,
  active,
  onSelect,
}: {
  question: Question;
  active: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "block w-full rounded-md border-l-4 px-3 py-2 text-left text-sm transition-colors",
        active
          ? "border-l-accent bg-accent/10 text-accent"
          : "border-l-transparent text-muted-foreground hover:border-l-border hover:bg-secondary/40 hover:text-foreground",
      )}
    >
      <span className="line-clamp-2 font-medium">{question.prompt}</span>
    </button>
  );
}

function Timeline({ questionId }: { questionId: string }) {
  const entries = useEntriesByQuestion(questionId);
  if (entries.isPending) {
    return <p className="text-sm text-muted-foreground">Loading…</p>;
  }
  if (entries.isError) {
    return (
      <p className="text-sm text-destructive">Couldn&apos;t load entries.</p>
    );
  }
  if (!entries.data || entries.data.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        No answers yet for this question.
      </p>
    );
  }
  return (
    <ol className="space-y-6">
      {entries.data.map((e) => (
        <li key={e.id} className="border-l-2 border-border/60 pl-4">
          <Link
            to={`/history/${e.local_date}`}
            className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground hover:text-accent"
          >
            {formatHumanDate(e.local_date)}
          </Link>
          <p className="mt-1 whitespace-pre-wrap text-body leading-prose">
            {e.body}
          </p>
        </li>
      ))}
    </ol>
  );
}
