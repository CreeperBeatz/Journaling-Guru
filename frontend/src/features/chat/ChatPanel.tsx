import { useCallback, useEffect, useMemo, useRef } from "react";

import { Card, CardContent } from "@/components/ui/card";
import { cn } from "@/lib/utils";

import {
  type ChatMessage,
  type ChatPhase,
  type ChatSession,
} from "./api";
import {
  useCreateOrResumeSession,
  useFinalizeChat,
  useResetChat,
  useStreamingChat,
  useTodayChatSession,
  visibleMessages,
} from "./hooks";
import { ComposerActions } from "./components/ComposerActions";
import { ComposerInput } from "./components/ComposerInput";
import { CoverageChips } from "./components/CoverageChips";
import { CrisisCard } from "./components/CrisisCard";
import { MessageList } from "./components/MessageList";

// All four Energy Audit topic codes. Mirrors backend
// internal/llm/chat/coverage.go::CoverageCodes.
const ALL_TOPIC_CODES = ["drained", "charged", "grateful", "else"] as const;

// "At bottom" tolerance for auto-follow: if the user is within this many
// pixels of the bottom, consider them "reading the latest" and re-anchor
// on every content change. Larger threshold = more lenient (auto-follows
// even when slightly scrolled up).
const AT_BOTTOM_PX = 80;

// Threshold for the auto-hide-on-scroll-down trigger. The banner stays
// visible until the user has scrolled down at least this far.
const HIDE_HEADER_AFTER_PX = 80;

// Minimum scroll-delta to react to (avoids jitter from inertial scroll
// rebound and pixel-rounded touchpads).
const SCROLL_DELTA_MIN_PX = 8;

interface Props {
  // Called when the chat's internal scroll direction changes meaningfully.
  // DailyEntry uses this to auto-hide the date banner.
  onHeaderHiddenChange?: (hidden: boolean) => void;
  // Whether the date banner is currently hidden. Drives the chat's `top`
  // offset so the chat surface gains the banner's height when collapsed.
  headerHidden?: boolean;
}

// ChatPanel is the chat-mode body of /today. Claude.ai-style: full-bleed
// fixed overlay between the AppShell's sidebar / mobile header / bottom
// tab bar. The message stream owns its own scroll container; its right
// edge coincides with the viewport right edge so the browser scrollbar
// appears there, NOT buried inside a centered max-width card.
//
// Fade + composer pill live INSIDE the scroll container as sticky-bottom
// elements so they pin to the bottom of the visible area without
// overlapping the scrollbar gutter. The natural-flow consequence: the
// composer's own height is part of the scrollable content, so
// scrollHeight already accounts for it — auto-scroll-to-bottom lands
// the messages at the visible region above the pinned composer.
export function ChatPanel({ onHeaderHiddenChange, headerHidden }: Props) {
  const sessionQuery = useTodayChatSession();
  const createOrResume = useCreateOrResumeSession();
  const finalize = useFinalizeChat();
  const resetChat = useResetChat();

  const session: ChatSession | null = sessionQuery.data?.session ?? null;
  const messages: ChatMessage[] = sessionQuery.data?.messages ?? [];

  const stream = useStreamingChat(session?.id ?? null);

  useEffect(() => {
    if (sessionQuery.isPending) return;
    if (sessionQuery.data && !sessionQuery.data.session) {
      createOrResume.mutate();
    }
  }, [sessionQuery.isPending, sessionQuery.data, createOrResume]);

  // Auto-stream the opener whenever we land in greeting-phase with no
  // assistant turn yet. Fires for fresh sessions AND after a Reset
  // (which flips phase back to greeting and empties messages on the
  // server). The guard is `status === "streaming"` rather than
  // `!== "idle"` so a leftover "done" status from the previous turn
  // doesn't block the post-reset opener.
  useEffect(() => {
    if (!session) return;
    if (session.phase !== "greeting") return;
    if (messages.some((m) => m.role === "assistant")) return;
    if (stream.state.status === "streaming") return;
    void stream.startOpener();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session?.id, session?.phase, messages.length, stream.state.status]);

  const visibleMsgs = useMemo(() => visibleMessages(messages), [messages]);

  const coveredCodes = useMemo(() => {
    return new Set<string>(session?.covered_question_ids ?? []);
  }, [session?.covered_question_ids]);

  // Scroll bookkeeping. Two derived signals:
  //   1. isAtBottomRef — controls auto-follow. We re-anchor to the bottom
  //      on every content change as long as the user hasn't scrolled away.
  //   2. lastY for the header-hide direction detector.
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const isAtBottomRef = useRef(true);
  const lastY = useRef(0);

  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const y = el.scrollTop;
    const distanceFromBottom = el.scrollHeight - el.clientHeight - y;
    isAtBottomRef.current = distanceFromBottom <= AT_BOTTOM_PX;

    if (!onHeaderHiddenChange) return;

    // Auto-follow / at-bottom: force banner hidden, ignore direction
    // entirely. This is the load-bearing fix for the flicker — token-
    // by-token programmatic scrolls during streaming were producing
    // tiny deltas in both directions that rapidly toggled the banner.
    if (isAtBottomRef.current && y > HIDE_HEADER_AFTER_PX) {
      onHeaderHiddenChange(true);
      lastY.current = y;
      return;
    }

    // Near the top: always show the banner. A user who's scrolled all
    // the way back up is reading from the start and shouldn't be
    // chasing a hidden header.
    if (y <= HIDE_HEADER_AFTER_PX) {
      onHeaderHiddenChange(false);
      lastY.current = y;
      return;
    }

    // Mid-scroll, not at-bottom: follow scroll direction.
    const delta = y - lastY.current;
    if (Math.abs(delta) >= SCROLL_DELTA_MIN_PX) {
      if (delta > 0) {
        onHeaderHiddenChange(true);
      } else {
        onHeaderHiddenChange(false);
      }
      lastY.current = y;
    }
  }, [onHeaderHiddenChange]);

  // Auto-follow: snap the scroll to the bottom whenever the visible
  // content grows AND the user was already at-or-near the bottom. Use
  // `behavior: 'auto'` (instant) so each token append doesn't kick off
  // a new smooth-scroll animation that interrupts the previous one —
  // smooth-during-streaming is what causes the visible flicker.
  useEffect(() => {
    if (!isAtBottomRef.current) return;
    const el = scrollRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [visibleMsgs.length, stream.state.partial.length, stream.state.status]);

  // First-mount + new-session anchor: jump to bottom (instant, no animation).
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
    isAtBottomRef.current = true;
  }, [session?.id]);

  // Once the persisted assistant turn lands (messages.length grows past
  // the count we recorded when `done` fired), clear the streaming
  // partial. This is the load-bearing fix for the end-of-stream
  // flicker — the streaming bubble stays mounted continuously from
  // first token through "persisted twin in cache", then atomically
  // hands off.
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

  if (sessionQuery.isPending) {
    return (
      <Card>
        <CardContent className="px-6 py-12 text-center text-sm text-muted-foreground">
          Loading conversation…
        </CardContent>
      </Card>
    );
  }
  if (sessionQuery.isError) {
    return (
      <Card>
        <CardContent className="px-6 py-8 text-sm text-destructive">
          Couldn&apos;t load chat: {sessionQuery.error.message}
        </CardContent>
      </Card>
    );
  }
  if (!session) {
    return (
      <Card>
        <CardContent className="px-6 py-8 text-sm text-muted-foreground">
          Starting a new conversation…
        </CardContent>
      </Card>
    );
  }

  const phase: ChatPhase = session.phase;
  const composerDisabled = stream.state.status === "streaming";
  const handleFinalize = () => {
    if (!session) return;
    finalize.mutate(session.id);
  };
  const handleReset = () => {
    if (!session) return;
    resetChat.mutate(session.id);
  };
  const handleWrapUp = () => {
    void stream.triggerWrapUp();
  };

  const userTurnCount = visibleMsgs.filter((m) => m.role === "user").length;
  const hasUserTurns = userTurnCount > 0;

  const remainingTopics = ALL_TOPIC_CODES.some((code) => !coveredCodes.has(code));
  const showWrapUp = hasUserTurns && remainingTopics && phase !== "wrapping_up";

  // Top offset values (banner shown vs hidden). The chat panel grows
  // upward as the banner collapses.
  const topOffset = headerHidden ? "3.5rem" : "9rem";

  return (
    <div
      className={cn(
        "fixed left-0 right-0 z-10 md:left-60",
        "bottom-14 md:bottom-0",
        "flex flex-col bg-background",
        "transition-[top] duration-200 ease-out",
      )}
      style={{
        top: `calc(var(--app-mobile-header-h, 0px) + ${topOffset})`,
      }}
    >
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        // Scroll container — full panel width so the scrollbar lands at
        // the viewport right edge.
        className="relative flex-1 min-h-0 overflow-y-auto"
      >
        {/* Inner flex column with min-h-full. When the conversation is
         *  short or empty, the messages div (flex-1) expands to fill
         *  available height and the sticky-bottom composer pins to the
         *  actual scroll-container bottom. Without min-h-full the
         *  composer would float up to wherever the (small) content
         *  ends. */}
        <div className="flex min-h-full flex-col">
          {/* Centered messages content. flex-1 so the column's free
           *  space is absorbed here, pushing the composer down. */}
          <div className="mx-auto w-full max-w-3xl flex-1 px-4 pt-2 pb-4 md:px-6">
            <MessageList
              messages={visibleMsgs}
              partial={stream.state.partial}
            />
            {stream.state.crisis ? (
              <div className="mt-4">
                <CrisisCard
                  resourcesUrl={stream.state.crisis.resources_url}
                  onResume={stream.dismissCrisis}
                />
              </div>
            ) : null}
          </div>

          {/* Sticky-bottom anchor for the composer pill. Lives inside
           *  the scroll container so the scrollbar gutter remains
           *  uncovered. */}
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
                {(coveredCodes.size > 0 || showWrapUp || hasUserTurns) ? (
                  <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2 pb-2">
                    <CoverageChips coveredCodes={coveredCodes} />
                    <ComposerActions
                      showWrapUp={showWrapUp}
                      wrapUpPending={composerDisabled}
                      busy={composerDisabled}
                      finishDisabled={!hasUserTurns}
                      restartDisabled={visibleMsgs.length === 0}
                      finishPending={finalize.isPending}
                      restartPending={resetChat.isPending}
                      onWrapUp={handleWrapUp}
                      onFinish={handleFinalize}
                      onRestart={handleReset}
                    />
                  </div>
                ) : null}
                <ComposerInput
                  onSend={stream.sendMessage}
                  disabled={composerDisabled || stream.state.crisis !== null}
                  pending={stream.state.status === "streaming"}
                  placeholder={placeholderForPhase(phase)}
                  bare
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

function placeholderForPhase(phase: ChatPhase): string {
  switch (phase) {
    case "greeting":
      return "Type your reply…";
    case "wrapping_up":
      return "One last thing on your mind?";
    default:
      return "Say something…";
  }
}
