import { useCallback, useEffect, useRef, useState } from "react";
import { AlertCircle, Loader2 } from "lucide-react";
import { LayoutGroup, motion, useReducedMotion } from "motion/react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { PullToRefresh } from "@/components/shell/PullToRefresh";
import { SwipeNavigator } from "@/components/shell/SwipeNavigator";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useMe } from "@/features/auth/useAuth";
import { ChatPanel } from "@/features/chat/ChatPanel";
import {
  isExtractionInFlight,
  useExtractionStatus,
  useFinalizeChat,
  useTodayChatSession,
} from "@/features/chat/hooks";
import { minutesToHHMM } from "@/lib/dayStart";
import { cn } from "@/lib/utils";

import { type JournalEntry, type Question, listEntries } from "./api";
import { useEntries, useQuestions, ENTRY_DATES_KEY, entriesKey } from "./hooks";
import { QuestionAnswer } from "./QuestionAnswer";
import { DailyInputs } from "@/features/daily/DailyInputs";
import { useDailyInput, useSaveDailyInput } from "@/features/daily/hooks";

// Three modes coexist on /today (Phase 6a). Talk is reserved for 6b
// (voice via OpenAI Realtime) and renders as a disabled "soon" tab.
type TodayMode = "manual" | "chat" | "talk";

const MODE_STORAGE_KEY = "journai.todayMode";

function isMode(s: string | null | undefined): s is TodayMode {
  return s === "manual" || s === "chat" || s === "talk";
}

// Default mode resolution: ?mode= URL param → localStorage → 'chat'.
// URL takes precedence so deep-links and reload-after-toggle stay
// stable; localStorage gives a "remember my last tab" feel; the chat-
// first default reflects the v6a thesis (engagement > data entry).
function readDefaultMode(urlMode: string | null): TodayMode {
  if (isMode(urlMode)) return urlMode;
  if (typeof window !== "undefined") {
    const stored = window.localStorage.getItem(MODE_STORAGE_KEY);
    if (isMode(stored)) return stored;
  }
  return "chat";
}

// useNow ticks every 30s — fine-grained enough that the clock visibly
// matches the wall and the rollover boundary appears live, without
// re-rendering the page every second.
function useNow(): Date {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const t = setInterval(() => setNow(new Date()), 30_000);
    return () => clearInterval(t);
  }, []);
  return now;
}

function formatHumanDate(yyyymmdd: string): string {
  const [y, m, d] = yyyymmdd.split("-").map(Number);
  if (!y || !m || !d) return yyyymmdd;
  const date = new Date(Date.UTC(y, m - 1, d));
  return date.toLocaleDateString(undefined, {
    weekday: "long",
    year: "numeric",
    month: "long",
    day: "numeric",
    timeZone: "UTC",
  });
}

// Condensed variant used by the stuck Today bar. "Thu May 7" instead of
// "Thursday, May 7, 2026" — keeps the strip to one line on narrow screens.
function formatShortDate(yyyymmdd: string): string {
  const [y, m, d] = yyyymmdd.split("-").map(Number);
  if (!y || !m || !d) return yyyymmdd;
  const date = new Date(Date.UTC(y, m - 1, d));
  return date.toLocaleDateString(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric",
    timeZone: "UTC",
  });
}

// Watches a sentinel element's vertical position relative to the
// viewport's sticky offset (AppShell mobile header = 56px on mobile,
// 0 on desktop). Returns `true` once the sentinel has scrolled above
// the threshold — i.e. the sticky bar that follows it is now pinned.
//
// Two design choices that prevent flicker at the boundary:
//   1. **Sentinel, not the bar.** Measuring the bar itself races with
//      its own collapse: when the bar shrinks, the page's scroll
//      origin shifts, which in pathological cases can push the bar's
//      rect.top back across the threshold and oscillate. The sentinel
//      is a 1px-tall element above the bar; its size never changes,
//      so its rect.top monotonically tracks scroll regardless of
//      what the bar is doing.
//   2. **Hysteresis (8px deadband).** Once stuck, we only un-stick
//      when the sentinel returns more than 8px above the trigger
//      line. Absorbs sub-pixel jitter and any residual layout shift
//      while the bar finishes its transition.
function useIsStuck(sentinelRef: React.RefObject<HTMLElement | null>): boolean {
  const [stuck, setStuck] = useState(false);
  useEffect(() => {
    const measure = () => {
      const el = sentinelRef.current;
      if (!el) return;
      // Read the same CSS var that drives the sticky bar's `top`, so
      // detection and rendering can't drift out of sync. Falls back to
      // 0 (desktop) when the var hasn't been published yet.
      const headerHRaw = getComputedStyle(
        document.documentElement,
      ).getPropertyValue("--app-mobile-header-h");
      const stickyTop = parseFloat(headerHRaw) || 0;
      const rect = el.getBoundingClientRect();
      setStuck((prev) =>
        prev ? rect.top < stickyTop + 8 : rect.top < stickyTop,
      );
    };
    measure();
    window.addEventListener("scroll", measure, { passive: true });
    window.addEventListener("resize", measure, { passive: true });
    return () => {
      window.removeEventListener("scroll", measure);
      window.removeEventListener("resize", measure);
    };
  }, [sentinelRef]);
  return stuck;
}

// Calendar arithmetic in UTC space — the local_date is a wall-clock day so
// "the day before" is a calendar subtraction, not a timezone shift.
function dayBefore(yyyymmdd: string): string | null {
  const [y, m, d] = yyyymmdd.split("-").map(Number);
  if (!y || !m || !d) return null;
  const date = new Date(Date.UTC(y, m - 1, d));
  date.setUTCDate(date.getUTCDate() - 1);
  const yy = date.getUTCFullYear();
  const mm = String(date.getUTCMonth() + 1).padStart(2, "0");
  const dd = String(date.getUTCDate()).padStart(2, "0");
  return `${yy}-${mm}-${dd}`;
}

export function DailyEntry() {
  const me = useMe();
  const questions = useQuestions();
  const entries = useEntries();
  const dailyInput = useDailyInput();
  const saveDaily = useSaveDailyInput();
  const navigate = useNavigate();
  const qc = useQueryClient();
  const now = useNow();

  const [searchParams, setSearchParams] = useSearchParams();
  const urlMode = searchParams.get("mode");
  const [mode, setMode] = useState<TodayMode>(() => readDefaultMode(urlMode));

  // Sync URL → state when the user types a different ?mode= or hits the
  // back button. Only rewrite state when the URL has a valid mode; if
  // the URL has none, leave the user on whatever they were last on.
  useEffect(() => {
    if (isMode(urlMode) && urlMode !== mode) {
      setMode(urlMode);
    }
  }, [urlMode, mode]);

  // Persist + reflect to URL when the user changes mode.
  const handleModeChange = useCallback(
    (next: string) => {
      if (!isMode(next) || next === "talk") return; // talk is disabled in 6a
      setMode(next);
      if (typeof window !== "undefined") {
        window.localStorage.setItem(MODE_STORAGE_KEY, next);
      }
      const params = new URLSearchParams(searchParams);
      params.set("mode", next);
      setSearchParams(params, { replace: true });
    },
    [searchParams, setSearchParams],
  );

  // The sticky Today bar collapses from a multi-line display header to a
  // thin one-line strip the moment the user scrolls past its natural
  // position. The same `isStuck` value flows into ChatPanel so its
  // CoverageChips compact at the same time, keeping both stickies
  // visually unified. Stuck-state is observed via a 1px sentinel
  // immediately before the bar (see useIsStuck for why).
  const sentinelRef = useRef<HTMLDivElement>(null);
  const isStuck = useIsStuck(sentinelRef);
  const prefersReducedMotion = useReducedMotion();
  const layoutAnim = prefersReducedMotion
    ? ({ duration: 0 } as const)
    : ({ type: "spring", stiffness: 380, damping: 34, mass: 0.7 } as const);

  // Background "Update check-in" status. Lives in DailyEntry (not
  // ChatPanel) so the sticky bar's "Updating…" pill is visible from
  // any mode and the polling effect (toast on completed/failed) only
  // fires from one place. ChatPanel's "Update check-in" button still
  // triggers the finalize via its own mutation hook; both observers
  // converge on the same react-query cache.
  const todayChat = useTodayChatSession();
  const chatSession = todayChat.data?.session ?? null;
  const finalizeRetry = useFinalizeChat();
  const extractionPolling = useExtractionStatus(
    chatSession?.id ?? null,
    isExtractionInFlight(chatSession?.extraction_status),
  );
  const extractionStatus =
    extractionPolling.data?.status ?? chatSession?.extraction_status ?? "idle";
  const extractionError =
    extractionPolling.data?.error ?? chatSession?.extraction_error ?? null;
  const handleRetryFinalize = () => {
    if (!chatSession) return;
    finalizeRetry.mutate(chatSession.id);
  };

  if (questions.isPending || entries.isPending) {
    return <p className="text-sm text-muted-foreground">Loading…</p>;
  }
  if (questions.isError) {
    return <p className="text-sm text-destructive">Couldn't load questions.</p>;
  }
  if (entries.isError) {
    return <p className="text-sm text-destructive">Couldn't load today's entries.</p>;
  }

  const today = entries.data?.local_date;
  const yesterday = today ? dayBefore(today) : null;
  const dayLabel = today ? formatHumanDate(today) : "Today";
  const dayShortLabel = today ? formatShortDate(today) : "Today";

  const entryByQuestion = new Map(
    (entries.data?.entries ?? []).map((e) => [e.question_id, e]),
  );
  const currentTime = me.data
    ? new Intl.DateTimeFormat(undefined, {
        hour: "2-digit",
        minute: "2-digit",
        hour12: false,
        timeZone: me.data.timezone,
      }).format(now)
    : null;

  const onRefresh = async () => {
    await Promise.all([
      qc.refetchQueries({ queryKey: entriesKey() }),
      qc.refetchQueries({ queryKey: ENTRY_DATES_KEY }),
    ]);
  };

  // Manual mode keeps the swipe-to-yesterday gesture; Chat mode skips
  // it because the message list scrolls and a horizontal-drag fight
  // would feel terrible.
  //
  // The sticky bar wraps the date header + mode tabs into one element
  // that pins to the top of the viewport (below AppShell's mobile
  // header) and condenses to a single thin strip once the user scrolls
  // past it. The bleed-out `-mx-4 md:-mx-8` matches AppShell's main
  // padding so the glass strip extends to the content frame edges.
  const body = (
    <Tabs value={mode} onValueChange={handleModeChange}>
      {/* 1px sentinel placed at the bar's natural top. Stuck-state
       * detection observes this element instead of the bar itself,
       * so the trigger doesn't depend on the bar's collapsing
       * geometry and can't oscillate at the boundary. */}
      <div ref={sentinelRef} aria-hidden className="h-px" />

      <div
        data-stuck={isStuck ? "true" : "false"}
        // Top offset is the AppShell mobile header's actual rendered
        // height (set as a CSS var by AppShell's ResizeObserver).
        // Resolves to 0 on desktop where the mobile header is
        // `md:hidden`, so the bar pins to viewport top there.
        style={{ top: "var(--app-mobile-header-h, 2.5rem)" }}
        className={cn(
          "sticky z-20",
          "-mx-4 md:-mx-8 px-4 md:px-8",
          "bg-background/85 backdrop-blur-md",
          "border-b transition-[border-color,padding] duration-200",
          isStuck ? "border-border/60 py-2" : "border-transparent py-4 md:py-5",
        )}
      >
        {/*
         * Layout strategy:
         *   Mobile + stuck → header is hidden, tabs become the bar.
         *     Avoids cramming a date AND tabs into 375px while keeping
         *     vertical real estate minimal.
         *   Desktop + stuck → flex-row with date · time on the left and
         *     tabs on the right (per the chosen mockup).
         *   Anywhere unstuck → flex-col with the full display header
         *     above the tabs.
         */}
        <LayoutGroup>
        <div
          className={cn(
            "flex flex-col gap-3",
            isStuck && "md:flex-row md:items-center md:gap-3",
          )}
        >
          <header
            className={cn(
              "min-w-0 flex-1 transition-[max-height,opacity] duration-200 overflow-hidden",
              !isStuck && "space-y-1",
              // Mobile + stuck: collapse header out of layout so tabs
              // sit flush at the top of the bar.
              isStuck ? "max-md:max-h-0 max-md:opacity-0" : "max-h-32 opacity-100",
              isStuck && "md:leading-tight",
            )}
          >
            {!isStuck ? (
              <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
                <h1 className="font-serif text-h1">{dayLabel}</h1>
                {me.data ? (
                  <p className="font-mono text-xs tabular-nums text-muted-foreground">
                    <span>{currentTime}</span> · {me.data.timezone} · new day at{" "}
                    {minutesToHHMM(me.data.day_start_minutes)}
                  </p>
                ) : null}
              </div>
            ) : (
              <h1 className="truncate font-sans text-sm font-medium leading-tight transition-[font-size,line-height] duration-200">
                {dayShortLabel}
                {currentTime ? (
                  <span className="ml-2 font-mono text-xs font-normal tabular-nums text-muted-foreground">
                    · {currentTime}
                  </span>
                ) : null}
              </h1>
            )}
          </header>

          {extractionStatus === "pending" || extractionStatus === "running" ? (
            <span
              role="status"
              aria-live="polite"
              className={cn(
                "inline-flex shrink-0 items-center gap-1.5 rounded-full bg-accent/15 px-3 py-1 text-xs font-medium text-accent",
                !isStuck && "self-end",
              )}
            >
              <Loader2 className="h-3 w-3 animate-spin" aria-hidden />
              Updating check-in…
            </span>
          ) : extractionStatus === "failed" ? (
            <button
              type="button"
              onClick={handleRetryFinalize}
              disabled={finalizeRetry.isPending}
              title={extractionError ?? "Extraction failed — tap to retry"}
              className={cn(
                "inline-flex shrink-0 items-center gap-1.5 rounded-full border border-destructive/40 bg-destructive/10 px-3 py-1 text-xs font-medium text-destructive transition-colors hover:bg-destructive/15 disabled:opacity-60",
                !isStuck && "self-end",
              )}
            >
              <AlertCircle className="h-3 w-3" aria-hidden />
              {finalizeRetry.isPending ? "Retrying…" : "Retry update"}
            </button>
          ) : null}

          <motion.div
            layout
            transition={layoutAnim}
            className={cn(
              "w-full",
              isStuck && "md:w-auto md:shrink-0 md:ml-auto",
            )}
          >
            <TabsList
              className={cn(
                "grid w-full grid-cols-3 transition-[height] duration-200",
                isStuck && "h-8 md:inline-flex md:w-auto",
              )}
            >
              <TabsTrigger value="manual">Manual</TabsTrigger>
              <TabsTrigger value="chat">Chat</TabsTrigger>
              <TabsTrigger value="talk" disabled className="gap-1.5">
                Talk
                <span className="rounded-full bg-muted-foreground/15 px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">
                  soon
                </span>
              </TabsTrigger>
            </TabsList>
          </motion.div>
        </div>
        </LayoutGroup>
      </div>

      <TabsContent value="manual" className="mt-6">
        <ManualBody
          questions={questions.data ?? []}
          entryByQuestion={entryByQuestion}
          dailyInput={dailyInput.data?.input ?? null}
          onSaveDaily={(body) => saveDaily.mutate(body)}
          isSavingDaily={saveDaily.isPending}
        />
      </TabsContent>

      <TabsContent value="chat" className="mt-6">
        <ChatPanel coverageCompact={isStuck} />
      </TabsContent>

      <TabsContent value="talk" className="mt-6">
        <Card>
          <CardContent className="px-6 py-10 text-sm text-muted-foreground">
            Voice mode is coming in the next phase. For now, use Chat or Manual.
          </CardContent>
        </Card>
      </TabsContent>
    </Tabs>
  );

  return (
    <PullToRefresh onRefresh={onRefresh}>
      {mode === "manual" ? (
        <SwipeNavigator
          onSwipeRight={() => {
            if (yesterday) navigate(`/history/${yesterday}`);
          }}
          onDragStart={() => {
            if (yesterday) {
              qc.prefetchQuery({
                queryKey: entriesKey(yesterday),
                queryFn: () => listEntries(yesterday),
              });
            }
          }}
        >
          {body}
        </SwipeNavigator>
      ) : (
        body
      )}
    </PullToRefresh>
  );
}

interface ManualBodyProps {
  questions: Question[];
  entryByQuestion: Map<string, JournalEntry>;
  dailyInput: Parameters<typeof DailyInputs>[0]["input"];
  onSaveDaily: Parameters<typeof DailyInputs>[0]["onSave"];
  isSavingDaily: boolean;
}

function ManualBody({
  questions,
  entryByQuestion,
  dailyInput,
  onSaveDaily,
  isSavingDaily,
}: ManualBodyProps) {
  return (
    <div className="space-y-6">
      <DailyInputs input={dailyInput} onSave={onSaveDaily} isSaving={isSavingDaily} />
      {questions.length === 0 ? (
        <Card>
          <CardHeader>
            <CardTitle className="font-serif">No questions yet</CardTitle>
            <CardDescription>
              You haven't set up any prompts. Add some to start journaling.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Button asChild>
              <Link to="/settings?tab=questions">Manage questions</Link>
            </Button>
          </CardContent>
        </Card>
      ) : (
        <div className="space-y-5">
          {questions.map((q) => (
            <QuestionAnswer
              key={q.id}
              question={q}
              entry={entryByQuestion.get(q.id)}
            />
          ))}
        </div>
      )}
    </div>
  );
}
