import { useCallback, useEffect, useMemo, useRef, useState } from "react";

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
import { ChatKebab, WrapUpButton } from "./components/ComposerActions";
import { ComposerInput } from "./components/ComposerInput";
import { CrisisCard } from "./components/CrisisCard";
import { MessageList } from "./components/MessageList";

// All four Energy Audit topic codes. Mirrors backend
// internal/llm/chat/coverage.go::CoverageCodes. Used to gate the
// Wrap-up CTA: once every topic is covered, we stop nudging.
const ALL_TOPIC_CODES = ["drained", "charged", "grateful", "else"] as const;

// "At bottom" tolerance for auto-follow: if the user is within this many
// pixels of the bottom, consider them "reading the latest" and re-anchor
// on every content change. Larger threshold = more lenient (auto-follows
// even when slightly scrolled up).
const AT_BOTTOM_PX = 80;

// If the only assistant turn (the opener) is older than this, refresh
// it. The opener's persona prompt embeds the current time of day
// ("good morning" / "good evening" / etc.), so a stale opener greets
// the user with the wrong tone if they open the app many hours after
// the session was first created. Only triggered when the user has
// not yet replied — once they've sent a message, the conversation is
// theirs and we don't rewind it.
const STALE_OPENER_MS = 4 * 60 * 60 * 1000;

// ChatPanel is the chat-mode body of /today. Claude.ai-style: full-bleed
// fixed overlay between the AppShell's sidebar / mobile header. The
// message stream owns its own scroll container; its right edge
// coincides with the viewport right edge so the browser scrollbar
// appears there, NOT buried inside a centered max-width card.
//
// Fade + composer pill live INSIDE the scroll container as sticky-bottom
// elements so they pin to the bottom of the visible area without
// overlapping the scrollbar gutter. The natural-flow consequence: the
// composer's own height is part of the scrollable content, so
// scrollHeight already accounts for it — auto-scroll-to-bottom lands
// the messages at the visible region above the pinned composer.
export function ChatPanel() {
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
  //
  // openerFiredForRef tracks the session id we've already kicked an
  // opener for, so we don't double-fire in the gap between the
  // streaming generator emitting `done` (status → "done") and the
  // session-envelope refetch landing the persisted assistant message
  // in the cache. Cleared on session.id change and on user-triggered
  // reset (handleReset below) so post-reset re-opening still works.
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

  // Stale-opener refresh: if the persisted opener is >4h old AND the
  // user hasn't replied yet, reset the session so the auto-opener
  // effect above streams a fresh greeting. Once-per-session via ref;
  // resetting clears `openerFiredForRef` so the next opener fires.
  const staleOpenerCheckedRef = useRef<string | null>(null);
  useEffect(() => {
    if (!session) return;
    if (session.phase !== "greeting") return;
    if (staleOpenerCheckedRef.current === session.id) return;
    if (resetChat.isPending) return;
    if (stream.state.status === "streaming") return;
    if (messages.some((m) => m.role === "user")) return;
    const assistantMsgs = messages.filter((m) => m.role === "assistant");
    if (assistantMsgs.length === 0) return;
    const opener = assistantMsgs[assistantMsgs.length - 1];
    const age = Date.now() - new Date(opener.created_at).getTime();
    if (age < STALE_OPENER_MS) return;
    staleOpenerCheckedRef.current = session.id;
    openerFiredForRef.current = null;
    resetChat.mutate(session.id);
  }, [session?.id, session?.phase, messages, stream.state.status, resetChat]);

  const visibleMsgs = useMemo(() => visibleMessages(messages), [messages]);

  const coveredCodes = useMemo(() => {
    return new Set<string>(session?.covered_question_ids ?? []);
  }, [session?.covered_question_ids]);

  // isAtBottomRef controls auto-follow — we re-anchor to the bottom on
  // every content change as long as the user hasn't scrolled away.
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const isAtBottomRef = useRef(true);

  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const distanceFromBottom = el.scrollHeight - el.clientHeight - el.scrollTop;
    isAtBottomRef.current = distanceFromBottom <= AT_BOTTOM_PX;
  }, []);

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
    openerFiredForRef.current = null;
    resetChat.mutate(session.id);
  };
  // wrapUpClicked drives the WrapUpButton's spinner so it only spins
  // when the user actually pressed it — not on every assistant reply.
  // Cleared once the wrap-up turn finishes streaming.
  const [wrapUpClicked, setWrapUpClicked] = useState(false);
  const handleWrapUp = () => {
    setWrapUpClicked(true);
    void stream.triggerWrapUp();
  };
  useEffect(() => {
    if (wrapUpClicked && stream.state.status !== "streaming") {
      setWrapUpClicked(false);
    }
  }, [wrapUpClicked, stream.state.status]);

  const userTurnCount = visibleMsgs.filter((m) => m.role === "user").length;
  const hasUserTurns = userTurnCount > 0;

  const remainingTopics = ALL_TOPIC_CODES.some((code) => !coveredCodes.has(code));
  const showWrapUp = hasUserTurns && remainingTopics && phase !== "wrapping_up";

  return (
    <div
      className={cn(
        "fixed inset-x-0 bottom-0 z-10 md:left-60",
        "flex flex-col bg-background",
      )}
      style={{
        // AppShell mobile header height + DailyEntry's sticky tab strip
        // (~3.5rem). On desktop the mobile header is `md:hidden` so the
        // CSS var resolves to 0 and only the tab strip offset applies.
        top: "calc(var(--app-mobile-header-h, 0px) + 3.5rem)",
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
                <ComposerInput
                  onSend={stream.sendMessage}
                  disabled={composerDisabled || stream.state.crisis !== null}
                  pending={stream.state.status === "streaming"}
                  placeholder={placeholderForPhase(phase)}
                  bare
                  bottomLeft={
                    <ChatKebab
                      finishDisabled={!hasUserTurns}
                      restartDisabled={visibleMsgs.length === 0}
                      finishPending={finalize.isPending}
                      restartPending={resetChat.isPending}
                      onFinish={handleFinalize}
                      onRestart={handleReset}
                    />
                  }
                  bottomRight={
                    showWrapUp ? (
                      <WrapUpButton
                        pending={wrapUpClicked}
                        disabled={composerDisabled}
                        onWrapUp={handleWrapUp}
                      />
                    ) : null
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
