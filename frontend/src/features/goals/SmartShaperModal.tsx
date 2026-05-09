import { useCallback, useEffect, useRef, useState } from "react";
import { Loader2, Send, Sparkles, X } from "lucide-react";

import { Button, buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { toast } from "@/components/ui/sonner";
import { cn } from "@/lib/utils";

import {
  CommitGoalArgs,
  DraftMessage,
  streamDraft,
} from "./api";
import { useCreateGoal } from "./hooks";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // Called after a goal is successfully created so the parent can close
  // the modal AND reset its own "creating" state.
  onCreated?: () => void;
  // When the user falls back to the manual form, the modal closes and
  // the parent surfaces the existing CreateGoalCard. Lets us avoid
  // duplicating the manual form here.
  onFallback?: () => void;
}

// SmartShaperModal — chat-first goal create flow. The shaper LLM asks
// clarifying questions until the goal is concrete, then emits a
// `commit_goal` tool call. The modal surfaces a "Save goal" CTA from
// the tool args; the user clicks it to actually persist the goal via
// the existing /api/goals POST.
//
// Implementation choices:
// - Stateless server: the FE owns the message transcript and resends
//   it on every turn. No session row.
// - Single SSE per turn (POST /api/goals/draft). The hook collects
//   tokens into a partial; on `done` it lands as a persisted assistant
//   turn in local state.
// - Tool args persist across turns until the user either Saves
//   (persists the goal) or revises (sends another user message,
//   which resets the tool args so we don't show stale ones).
export function SmartShaperModal({ open, onOpenChange, onCreated, onFallback }: Props) {
  const [messages, setMessages] = useState<DraftMessage[]>([]);
  const [partial, setPartial] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [pendingTool, setPendingTool] = useState<CommitGoalArgs | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const create = useCreateGoal();

  const reset = useCallback(() => {
    abortRef.current?.abort();
    abortRef.current = null;
    setMessages([]);
    setPartial("");
    setPending(false);
    setError(null);
    setPendingTool(null);
  }, []);

  // When the modal opens, fire the opener (server-injected welcoming
  // first message). When it closes, abort + reset.
  useEffect(() => {
    if (!open) {
      reset();
      return;
    }
    void runTurn([], true);
    return () => {
      abortRef.current?.abort();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const runTurn = useCallback(
    async (transcript: DraftMessage[], opener: boolean) => {
      if (pending) return;
      abortRef.current?.abort();
      const ac = new AbortController();
      abortRef.current = ac;
      setPending(true);
      setPartial("");
      setError(null);

      let buf = "";
      let tool: CommitGoalArgs | null = null;
      try {
        for await (const ev of streamDraft(transcript, ac.signal, opener)) {
          if (ev.kind === "token") {
            buf += ev.delta;
            setPartial(buf);
          } else if (ev.kind === "tool") {
            if (ev.name === "commit_goal") {
              const args = ev.args as Partial<CommitGoalArgs>;
              if (
                typeof args.title === "string" &&
                typeof args.check_in_question === "string" &&
                typeof args.duration_weeks === "number"
              ) {
                tool = {
                  title: args.title,
                  check_in_question: args.check_in_question,
                  duration_weeks: args.duration_weeks,
                };
              }
            }
          } else if (ev.kind === "error") {
            setError(ev.message);
          } else if (ev.kind === "done") {
            // handled below — drain the loop first
          }
        }
        // Stream ended. Persist the assistant turn if we got any text.
        if (buf.trim()) {
          setMessages((prev) => [...prev, { role: "assistant", content: buf.trim() }]);
        }
        if (tool) {
          setPendingTool(tool);
        }
      } catch (err) {
        if ((err as { name?: string })?.name === "AbortError") return;
        const msg = err instanceof Error ? err.message : "shaper failed";
        setError(msg);
      } finally {
        setPartial("");
        setPending(false);
      }
    },
    [pending],
  );

  const handleSendUser = useCallback(
    async (content: string) => {
      const trimmed = content.trim();
      if (!trimmed || pending) return;
      const next: DraftMessage[] = [...messages, { role: "user", content: trimmed }];
      setMessages(next);
      // Sending a new user turn invalidates any tool args from the
      // previous round — the user is revising, so the SHAPER decides
      // again.
      setPendingTool(null);
      await runTurn(next, false);
    },
    [messages, pending, runTurn],
  );

  const handleCommit = () => {
    if (!pendingTool) return;
    const today = new Date();
    const end = new Date(today);
    end.setDate(end.getDate() + pendingTool.duration_weeks * 7);
    const endDate = end.toISOString().slice(0, 10);
    create.mutate(
      {
        title: pendingTool.title,
        check_in_question: pendingTool.check_in_question,
        end_date: endDate,
      },
      {
        onSuccess: () => {
          toast.success("Goal created");
          onCreated?.();
          onOpenChange(false);
        },
      },
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle className="font-serif text-lg">Shape a new goal</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3">
          <div className="max-h-[50vh] min-h-[14rem] overflow-y-auto rounded-md border border-border/60 bg-muted/30 p-3">
            <Transcript messages={messages} partial={partial} />
            {error ? (
              <p className="mt-2 text-xs text-destructive/80">{error}</p>
            ) : null}
          </div>

          {pendingTool ? (
            <CommitCard
              args={pendingTool}
              pending={create.isPending}
              onCommit={handleCommit}
              onRevise={() => setPendingTool(null)}
            />
          ) : null}

          <Composer disabled={pending || create.isPending} onSend={handleSendUser} />

          <div className="flex items-center justify-between gap-2 pt-1 text-xs text-muted-foreground">
            <button
              type="button"
              onClick={() => {
                onOpenChange(false);
                onFallback?.();
              }}
              className="underline-offset-2 hover:underline"
            >
              Skip the shaper, fill the form manually
            </button>
            <span className="inline-flex items-center gap-1">
              <Sparkles className="h-3 w-3" /> SMART-shape mode
            </span>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function Transcript({
  messages,
  partial,
}: {
  messages: DraftMessage[];
  partial: string;
}) {
  return (
    <div className="space-y-3 text-sm">
      {messages.map((m, i) => (
        <Bubble key={i} role={m.role}>
          {m.content}
        </Bubble>
      ))}
      {partial.length > 0 ? <Bubble role="assistant">{partial}</Bubble> : null}
      {messages.length === 0 && partial === "" ? (
        <p className="italic text-muted-foreground">
          Starting up…
        </p>
      ) : null}
    </div>
  );
}

function Bubble({ role, children }: { role: "user" | "assistant"; children: React.ReactNode }) {
  return (
    <div className={cn("flex w-full", role === "user" ? "justify-end" : "justify-start")}>
      <div
        className={cn(
          "max-w-[88%] rounded-2xl px-3 py-2 text-sm leading-relaxed",
          role === "user"
            ? "border border-accent/30 bg-accent/15 text-foreground"
            : "border border-border/50 bg-card text-card-foreground",
        )}
      >
        <p className="whitespace-pre-wrap [overflow-wrap:anywhere]">{children}</p>
      </div>
    </div>
  );
}

function Composer({
  disabled,
  onSend,
}: {
  disabled: boolean;
  onSend: (content: string) => void;
}) {
  const [value, setValue] = useState("");
  return (
    <div className="flex items-end gap-2 rounded-md border border-border/70 bg-background px-2 py-1.5 focus-within:border-foreground/30">
      <Input
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            onSend(value);
            setValue("");
          }
        }}
        placeholder="Type your reply…"
        disabled={disabled}
        className="border-0 bg-transparent px-1 text-sm focus-visible:ring-0 focus-visible:ring-offset-0"
      />
      <Button
        type="button"
        size="icon"
        onClick={() => {
          onSend(value);
          setValue("");
        }}
        disabled={disabled || value.trim().length === 0}
        aria-label="Send"
      >
        {disabled ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}
      </Button>
    </div>
  );
}

function CommitCard({
  args,
  pending,
  onCommit,
  onRevise,
}: {
  args: CommitGoalArgs;
  pending: boolean;
  onCommit: () => void;
  onRevise: () => void;
}) {
  const today = new Date();
  const end = new Date(today);
  end.setDate(end.getDate() + args.duration_weeks * 7);
  return (
    <Card className="border-accent/40 bg-accent/5">
      <CardHeader className="pb-2">
        <CardTitle className="font-serif text-sm">Ready to save</CardTitle>
      </CardHeader>
      <CardContent className="space-y-2 text-sm">
        <Field label="Title" value={args.title} />
        <Field label="Daily check-in" value={args.check_in_question} />
        <Field
          label="Runs for"
          value={`${args.duration_weeks} week${args.duration_weeks === 1 ? "" : "s"} · ends ${end
            .toISOString()
            .slice(0, 10)}`}
        />
        <div className="flex gap-2 pt-2">
          <Button size="sm" onClick={onCommit} disabled={pending}>
            {pending ? "Saving…" : "Save goal"}
          </Button>
          <button
            type="button"
            onClick={onRevise}
            disabled={pending}
            className={cn(
              buttonVariants({ size: "sm", variant: "ghost" }),
              "gap-1 text-xs",
            )}
          >
            <X className="h-3 w-3" /> Keep tweaking
          </button>
        </div>
      </CardContent>
    </Card>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <p className="text-[11px] uppercase tracking-wider text-muted-foreground">
        {label}
      </p>
      <p className="text-sm">{value}</p>
    </div>
  );
}
