import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Loader2 } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { cn } from "@/lib/utils";

import type { ChatMessage } from "@/features/chat/api";
import {
  useCancelWrapUp,
  useFinalizeChat,
  useResetChat,
  useStreamingChat,
} from "@/features/chat/hooks";
import { ChatKebab, WrapUpButton } from "@/features/chat/components/ComposerActions";
import { ComposerInput } from "@/features/chat/components/ComposerInput";
import { CrisisCard } from "@/features/chat/components/CrisisCard";
import { MessageList } from "@/features/chat/components/MessageList";
import {
  ProposeCompleteGoalCard,
  type ProposeCompleteGoalArgs,
} from "./components/ProposeCompleteGoalCard";
import {
  ProposeExtendGoalCard,
  type ProposeExtendGoalArgs,
} from "./components/ProposeExtendGoalCard";
import {
  ProposeGoalCard,
  type ProposeGoalArgs,
} from "./components/ProposeGoalCard";
import { WeeklyChatFirstTimeHint } from "./components/WeeklyChatFirstTimeHint";
import {
  useCreateOrResumeWeeklyChat,
  useThisWeekReflection,
  useWeeklyChatSession,
  weeklyChatKey,
} from "./hooks";
import { resolveProposalDecisions } from "./proposalDecisions";

const AT_BOTTOM_PX = 80;

// weeklyVisibleMessages keeps user/assistant rows for the bubble list,
// plus assistant rows whose tool_name is one of the goal-proposal tools
// even when content is empty — those rows are still rendered (the
// MessageList drops the bubble when content is empty and just renders
// the inline card).
function weeklyVisibleMessages(messages: ChatMessage[]): ChatMessage[] {
  return messages.filter((m) => {
    if (m.role === "user") return true;
    if (m.role !== "assistant") return false;
    if (m.content.trim() !== "") return true;
    return (
      m.tool_name === "propose_goal" ||
      m.tool_name === "propose_extend_goal" ||
      m.tool_name === "propose_complete_goal"
    );
  });
}

interface Props {
  /** Called when the user finalizes the reflection chat. The host page
   * uses it to switch to the Summary tab. */
  onFinished?: () => void;
}

// WeeklyChat is the "Reflection" tab content on /weekly. Reuses the
// daily ChatPanel layout idiom (full-bleed fixed overlay below the
// AppShell chrome + tab strip) with weekly-scoped session hooks and
// inline goal-proposal cards.
export function WeeklyChat({ onFinished }: Props = {}) {
  const sessionQuery = useWeeklyChatSession();
  const createOrResume = useCreateOrResumeWeeklyChat();
  const finalize = useFinalizeChat(weeklyChatKey);
  const cancelWrap = useCancelWrapUp(weeklyChatKey);
  const resetChat = useResetChat(weeklyChatKey);
  const reflectionQuery = useThisWeekReflection();

  const session = sessionQuery.data?.session ?? null;
  const messages: ChatMessage[] = sessionQuery.data?.messages ?? [];

  const stream = useStreamingChat(session?.id ?? null, weeklyChatKey);

  // Lazy-create the session on first mount when none exists.
  //
  // Gated on createOrResume.isIdle so a single 500 from the BE doesn't
  // become a runaway loop: the mutation hook's identity changes every
  // render, so without this guard each re-render re-runs the effect
  // and re-POSTs. Once we leave idle the user must click "Try again"
  // (rendered below) to retry.
  useEffect(() => {
    if (sessionQuery.isPending) return;
    if (!sessionQuery.data || sessionQuery.data.session) return;
    if (!createOrResume.isIdle) return;
    createOrResume.mutate();
  }, [sessionQuery.isPending, sessionQuery.data, createOrResume]);

  // Auto-stream opener for fresh greeting-phase sessions.
  const openerFiredForRef = useRef<string | null>(null);
  useEffect(() => {
    openerFiredForRef.current = null;
  }, [session?.id]);
  useEffect(() => {
    if (!session) return;
    if (session.phase !== "greeting") return;
    if (messages.some((m) => m.role === "assistant")) return;
    if (stream.state.status === "streaming") return;
    if (openerFiredForRef.current === session.id) return;
    openerFiredForRef.current = session.id;
    void stream.startOpener();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session?.id, session?.phase, messages.length, stream.state.status]);

  const visibleMsgs = useMemo(() => weeklyVisibleMessages(messages), [messages]);

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
  }, [visibleMsgs.length, stream.state.partial.length, stream.state.status]);
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
    isAtBottomRef.current = true;
  }, [session?.id]);

  // Once the persisted assistant turn lands, clear the streaming partial.
  const partialDoneAnchorRef = useRef<number | null>(null);
  const visibleMsgsLen = visibleMsgs.length;
  const streamStatus = stream.state.status;
  const partialLen = stream.state.partial.length;
  useEffect(() => {
    if (streamStatus === "streaming") {
      partialDoneAnchorRef.current = null;
      return;
    }
    if (streamStatus !== "done" || partialLen === 0) return;
    if (partialDoneAnchorRef.current === null) {
      partialDoneAnchorRef.current = visibleMsgsLen;
      return;
    }
    if (visibleMsgsLen > partialDoneAnchorRef.current) {
      stream.clearPartial();
      partialDoneAnchorRef.current = null;
    }
  }, [streamStatus, partialLen, visibleMsgsLen, stream]);

  // Wrap-up affordance: the assistant has emitted propose_wrap_up (and
  // it hasn't been cancelled). Same logic as ChatPanel — read latest
  // tool_name from persisted transcript + live tool events.
  const modelProposedWrapUp = useMemo(() => {
    let proposed = false;
    for (const m of messages) {
      if (m.role === "assistant" && m.tool_name === "propose_wrap_up") {
        proposed = true;
      } else if (m.role === "system_event" && m.content === "user_cancel_wrap_up") {
        proposed = false;
      }
    }
    if (!proposed) {
      for (const t of stream.state.toolEvents) {
        if (t.name === "propose_wrap_up") {
          proposed = true;
          break;
        }
      }
    }
    return proposed;
  }, [messages, stream.state.toolEvents]);

  const [wrapUpClicked, setWrapUpClicked] = useState(false);
  useEffect(() => {
    if (wrapUpClicked && stream.state.status !== "streaming") {
      setWrapUpClicked(false);
    }
  }, [wrapUpClicked, stream.state.status]);

  // Goal-title lookup for the extend/complete cards. The reflection
  // response carries active_goals at week_end; that's the same set the
  // server seeded the system prompt with.
  const goalTitleByID = useMemo(() => {
    const m = new Map<string, string>();
    for (const g of reflectionQuery.data?.active_goals ?? []) {
      m.set(g.id, g.title);
    }
    return m;
  }, [reflectionQuery.data?.active_goals]);

  // Decisions are derived from the transcript so propose_* cards
  // survive a page refresh in their saved/declined state. Pure
  // function over the visible message list.
  const decisions = useMemo(() => resolveProposalDecisions(messages), [messages]);

  const renderToolCard = useCallback(
    (msg: ChatMessage) => {
      if (!session) return null;
      if (msg.role !== "assistant" || !msg.tool_name || !msg.tool_args) return null;
      const args = msg.tool_args as Record<string, unknown>;
      const decision = decisions.get(msg.id);
      switch (msg.tool_name) {
        case "propose_goal":
          return (
            <ProposeGoalCard
              sessionId={session.id}
              args={args as ProposeGoalArgs}
              decision={decision}
            />
          );
        case "propose_extend_goal": {
          const extendArgs = args as ProposeExtendGoalArgs;
          return (
            <ProposeExtendGoalCard
              sessionId={session.id}
              args={extendArgs}
              goalTitle={extendArgs.goal_id ? goalTitleByID.get(extendArgs.goal_id) : undefined}
              decision={decision}
            />
          );
        }
        case "propose_complete_goal": {
          const completeArgs = args as ProposeCompleteGoalArgs;
          return (
            <ProposeCompleteGoalCard
              sessionId={session.id}
              args={completeArgs}
              goalTitle={completeArgs.goal_id ? goalTitleByID.get(completeArgs.goal_id) : undefined}
              decision={decision}
            />
          );
        }
        default:
          return null;
      }
    },
    [session, goalTitleByID, decisions],
  );

  if (sessionQuery.isPending) {
    return (
      <div className="px-6 py-12 text-center text-sm text-muted-foreground">
        Loading conversation…
      </div>
    );
  }
  if (sessionQuery.isError) {
    return (
      <div className="px-6 py-8 text-sm text-destructive">
        Couldn&apos;t load chat: {sessionQuery.error.message}
      </div>
    );
  }
  if (createOrResume.isError) {
    return (
      <div className="space-y-3 px-6 py-8">
        <p className="text-sm text-destructive">
          Couldn&apos;t start your weekly reflection: {createOrResume.error.message}
        </p>
        <Button
          type="button"
          size="sm"
          onClick={() => createOrResume.mutate()}
        >
          Try again
        </Button>
      </div>
    );
  }
  if (!session) {
    return (
      <div className="px-6 py-8 text-sm text-muted-foreground">
        Starting your weekly reflection…
      </div>
    );
  }

  const composerDisabled = stream.state.status === "streaming";
  const handleFinalize = async () => {
    try {
      await finalize.mutateAsync({ sessionId: session.id });
      onFinished?.();
    } catch {
      /* toast already surfaced */
    }
  };
  const handleCancelWrapUp = () => {
    cancelWrap.mutate(session.id);
  };
  const handleWrapUp = () => {
    setWrapUpClicked(true);
    void stream.triggerWrapUp();
  };
  const handleRestart = () => {
    if (!session) return;
    // The kebab's AlertDialog has already confirmed.
    openerFiredForRef.current = null;
    resetChat.mutate(session.id);
  };

  const userTurnCount = visibleMsgs.filter((m) => m.role === "user").length;
  const hasUserTurns = userTurnCount > 0;
  const wrappedUp = session.phase === "wrapping_up";

  return (
    <div
      className={cn(
        "fixed inset-x-0 bottom-0 z-10 md:left-60",
        "flex flex-col bg-background",
      )}
      style={{
        // AppShell mobile-header height + the WeeklyReflection tab
        // strip (~3.5rem). Mirrors the daily ChatPanel pattern so the
        // tab strip stays clickable above the chat overlay.
        top: "calc(var(--app-mobile-header-h, 0px) + 3.5rem)",
      }}
    >
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="relative flex-1 min-h-0 overflow-y-auto"
      >
        <div className="flex min-h-full flex-col">
          <div className="mx-auto w-full max-w-3xl flex-1 px-4 pt-6 pb-4 md:px-6">
            <WeeklyChatFirstTimeHint />
            <MessageList
              messages={visibleMsgs}
              partial={stream.state.partial}
              renderToolCard={renderToolCard}
            />
            {stream.state.crisis ? (
              <div className="mt-4">
                <CrisisCard
                  resourcesUrl={stream.state.crisis.resources_url}
                  onResume={stream.dismissCrisis}
                />
              </div>
            ) : null}
            {modelProposedWrapUp && !stream.state.crisis ? (
              <Card className="mt-4 border-accent/30 bg-accent/5">
                <CardContent className="flex flex-col gap-2 px-4 py-3 text-sm sm:flex-row sm:items-center sm:justify-between">
                  <p>That feels like a good stopping point. Ready to finish your weekly reflection?</p>
                  <Button onClick={handleFinalize} disabled={finalize.isPending}>
                    {finalize.isPending ? (
                      <span className="inline-flex items-center gap-2">
                        <Loader2 className="h-4 w-4 animate-spin" />
                        Finishing…
                      </span>
                    ) : (
                      "Finish weekly reflection"
                    )}
                  </Button>
                </CardContent>
              </Card>
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
                  onSend={stream.sendMessage}
                  disabled={composerDisabled || stream.state.crisis !== null}
                  pending={stream.state.status === "streaming"}
                  placeholder="Say something…"
                  bare
                  bottomLeft={
                    <ChatKebab
                      finishDisabled={!hasUserTurns}
                      restartDisabled={visibleMsgs.length === 0}
                      finishPending={finalize.isPending}
                      restartPending={resetChat.isPending}
                      onFinish={handleFinalize}
                      onRestart={handleRestart}
                      wrappedUp={wrappedUp}
                      cancelWrapUpPending={cancelWrap.isPending}
                      onCancelWrapUp={handleCancelWrapUp}
                    />
                  }
                  bottomRight={
                    <WrapUpButton
                      pending={wrapUpClicked}
                      disabled={composerDisabled || !hasUserTurns}
                      wrappedUp={wrappedUp}
                      onWrapUp={handleWrapUp}
                    />
                  }
                />
                {stream.state.lastError ? (
                  <p className="px-1 pt-1 text-xs text-destructive/80">
                    {stream.state.lastError}
                  </p>
                ) : null}
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
