import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";

import { Button, buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { toast } from "@/components/ui/sonner";
import { cn } from "@/lib/utils";

import type { ChatMessage } from "@/features/chat/api";
import { MessageList } from "@/features/chat/components/MessageList";
import { MessageBubble } from "@/features/chat/components/MessageBubble";
import { ComposerInput } from "@/features/chat/components/ComposerInput";

import {
  CommitGoalArgs,
  DraftMessage,
  GoalSuggestion,
  streamDraft,
  suggestGoals,
  type SuggestGoalsResponse,
} from "./api";
import { useCreateGoal } from "./hooks";

// SmartShaperInline — chat-shaped goal shaper used by both the /goals
// modal and the weekly wizard's "Shape what's next" card. Fills its
// parent container's height: scroll area on top, sticky composer pill
// pinned to the bottom (matches /today's ChatPanel layout).
//
// The empty state shows up to 3 LLM-derived goal suggestion chips
// (fetched on mount via /api/goals/suggest). Clicking a chip seeds a
// pre-shaped user message; typing also dismisses the empty state. Once
// the conversation has any turn, the chips never reappear.
//
// If the suggest call fails or returns no chips, the inline falls back
// to the server's streaming opener (the model writes the first turn
// itself), preserving the prior behaviour.

const AT_BOTTOM_PX = 80;

interface Props {
  onCreated?: (goal: { id: string }) => void;
  onSkip?: () => void;
  // Override the skip label. Defaults to the wizard wording.
  skipLabel?: string;
}

// Synthesize a `ChatMessage`-shaped object so MessageBubble can render
// our shaper transcript. The bubble only reads role+content; the rest
// is filler. Index-based ids are stable React keys (transcript is
// append-only within one shaper session).
function toChatMessage(m: DraftMessage, idx: number): ChatMessage {
  return {
    id: `shaper-${idx}`,
    session_id: "shaper",
    seq: idx,
    role: m.role,
    content: m.content,
    token_in: 0,
    token_out: 0,
    created_at: new Date().toISOString(),
  };
}

export function SmartShaperInline({
  onCreated,
  onSkip,
  skipLabel = "Skip — no new goal this week",
}: Props) {
  const [messages, setMessages] = useState<DraftMessage[]>([]);
  const [partial, setPartial] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [pendingTool, setPendingTool] = useState<CommitGoalArgs | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const create = useCreateGoal();

  // Suggestions are fetched once and cached; both entry points (/goals
  // and the wizard) hit the same query key, so opening the wizard right
  // after using the goals modal is free.
  const suggest = useQuery<SuggestGoalsResponse>({
    queryKey: ["goals", "suggest"],
    queryFn: suggestGoals,
    staleTime: Infinity,
    retry: 0,
  });
  const chips: GoalSuggestion[] = suggest.data?.suggestions ?? [];
  const hasChips = chips.length > 0;

  const runTurn = useCallback(
    async (transcript: DraftMessage[], opener: boolean) => {
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
          }
        }
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
    [],
  );

  // Mount-time decision: wait for suggest to settle, then either let the
  // chips empty state stand, or stream the server opener as a fallback.
  // mountActionRef makes this idempotent across StrictMode double-mount
  // and dev re-renders.
  const mountActionRef = useRef<"pending" | "chips" | "opener">("pending");
  useEffect(() => {
    if (mountActionRef.current !== "pending") return;
    if (suggest.isPending) return;
    if (hasChips) {
      mountActionRef.current = "chips";
      return;
    }
    mountActionRef.current = "opener";
    void runTurn([], true);
    return () => {
      abortRef.current?.abort();
    };
  }, [suggest.isPending, hasChips, runTurn]);

  const handleSendUser = useCallback(
    async (content: string) => {
      const trimmed = content.trim();
      if (!trimmed || pending) return;
      const next: DraftMessage[] = [...messages, { role: "user", content: trimmed }];
      setMessages(next);
      setPendingTool(null);
      await runTurn(next, false);
    },
    [messages, pending, runTurn],
  );

  const handleChip = useCallback(
    (s: GoalSuggestion) => {
      const reply =
        `Let's go with: "${s.title}" — daily check-in "${s.check_in_question}" ` +
        `for ${s.duration_weeks} week${s.duration_weeks === 1 ? "" : "s"}.`;
      void handleSendUser(reply);
    },
    [handleSendUser],
  );

  const handleCommit = () => {
    if (!pendingTool) return;
    create.mutate(
      {
        title: pendingTool.title,
        check_in_question: pendingTool.check_in_question,
        duration_weeks: pendingTool.duration_weeks,
      },
      {
        onSuccess: (goal) => {
          toast.success("Goal created");
          onCreated?.({ id: goal.id });
        },
      },
    );
  };

  // Auto-follow scroll: re-anchor to the bottom on every content change
  // unless the user has scrolled away (>80px from bottom). Same pattern
  // as ChatPanel.
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const isAtBottomRef = useRef(true);
  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.clientHeight - el.scrollTop;
    isAtBottomRef.current = distanceFromBottom <= AT_BOTTOM_PX;
  }, []);
  useEffect(() => {
    if (!isAtBottomRef.current) return;
    const el = scrollRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [messages.length, partial.length, pendingTool]);

  const chatMessages = useMemo(
    () => messages.map((m, idx) => toChatMessage(m, idx)),
    [messages],
  );

  const showEmptyState = messages.length === 0 && partial.length === 0 && !pendingTool;
  const showChipsEmpty = showEmptyState && hasChips;
  const showSuggestPending =
    showEmptyState && suggest.isPending && mountActionRef.current === "pending";

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="relative flex-1 min-h-0 overflow-y-auto"
      >
        <div className="flex min-h-full flex-col">
          <div className="mx-auto w-full max-w-3xl flex-1 px-4 pt-4 pb-4 md:px-6">
            {showSuggestPending ? (
              <p className="italic text-muted-foreground">
                Looking for ideas based on your week…
              </p>
            ) : null}

            {showChipsEmpty ? (
              <div className="flex flex-col gap-3">
                <MessageBubble
                  message={toChatMessage(
                    {
                      role: "assistant",
                      content:
                        "I noticed your week. Want to try one of these, or shape something different?",
                    },
                    0,
                  )}
                />
                <div className="flex flex-wrap gap-2 pl-1">
                  {chips.map((c, j) => (
                    <button
                      key={j}
                      type="button"
                      onClick={() => handleChip(c)}
                      disabled={pending || create.isPending}
                      className={cn(
                        "rounded-full border border-accent/40 bg-accent/10 px-3 py-1 text-xs",
                        "transition-colors hover:bg-accent/20 focus:outline-none",
                        "focus-visible:ring-2 focus-visible:ring-accent",
                        "disabled:opacity-60",
                      )}
                      title={c.rationale}
                    >
                      {c.title}
                      <span className="ml-1 text-muted-foreground">
                        · {c.duration_weeks}w
                      </span>
                    </button>
                  ))}
                </div>
              </div>
            ) : null}

            {!showEmptyState ? (
              <MessageList messages={chatMessages} partial={partial} />
            ) : null}

            {pendingTool ? (
              <div className="mt-4">
                <CommitCard
                  args={pendingTool}
                  pending={create.isPending}
                  onCommit={handleCommit}
                  onRevise={() => setPendingTool(null)}
                />
              </div>
            ) : null}

            {error ? (
              <p className="mt-3 text-xs text-destructive/80">{error}</p>
            ) : null}
          </div>

          <div className="sticky bottom-0 z-10">
            <div className="flex justify-center bg-transparent px-3 pb-4 md:px-4">
              <div
                className={cn(
                  "pointer-events-auto w-full max-w-2xl",
                  "rounded-2xl border border-border/70",
                  "bg-background/85 shadow-lg backdrop-blur-md",
                  "px-3 pt-3 pb-2",
                )}
              >
                <ComposerInput
                  onSend={handleSendUser}
                  disabled={pending || create.isPending}
                  pending={pending}
                  placeholder="Type your reply…"
                  bare
                  bottomLeft={
                    onSkip ? (
                      <button
                        type="button"
                        onClick={onSkip}
                        disabled={pending || create.isPending}
                        className={cn(
                          "rounded-full px-3 py-1 text-xs text-muted-foreground",
                          "transition-colors hover:bg-muted/40 hover:text-foreground",
                          "focus:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                          "disabled:opacity-50",
                        )}
                      >
                        {skipLabel}
                      </button>
                    ) : null
                  }
                />
              </div>
            </div>
          </div>
        </div>
      </div>
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
          value={`${args.duration_weeks} week${args.duration_weeks === 1 ? "" : "s"} · ends on your reflection day`}
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
