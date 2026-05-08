import { useCallback, useEffect, useState } from "react";
import { AlertCircle, Loader2 } from "lucide-react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";

import { Card, CardContent } from "@/components/ui/card";
import { cn } from "@/lib/utils";
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

import { listEntries } from "./api";
import { useEntries, ENTRY_DATES_KEY, entriesKey } from "./hooks";
import { DailyInputs } from "@/features/daily/DailyInputs";
import type { DailyInput, DailyInputUpsertBody, TagDayLink } from "@/features/daily/api";
import { useDailyInput, useSaveDailyInput } from "@/features/daily/hooks";
import { GoalCheckInBlock } from "@/features/goals/GoalCheckInBlock";

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
  const entries = useEntries();
  const dailyInput = useDailyInput();
  const saveDaily = useSaveDailyInput();
  // Auto-hide the date banner when the chat scrolls down. ChatPanel
  // attaches a scroll listener and calls back with a hidden flag.
  // Reset to visible whenever the user switches modes — sticking
  // hidden across a Manual ↔ Chat round-trip would be confusing.
  const [headerHidden, setHeaderHidden] = useState(false);
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
      setHeaderHidden(false);
      if (typeof window !== "undefined") {
        window.localStorage.setItem(MODE_STORAGE_KEY, next);
      }
      const params = new URLSearchParams(searchParams);
      params.set("mode", next);
      setSearchParams(params, { replace: true });
    },
    [searchParams, setSearchParams],
  );

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

  if (entries.isPending) {
    return <p className="text-sm text-muted-foreground">Loading…</p>;
  }
  if (entries.isError) {
    return <p className="text-sm text-destructive">Couldn't load today's entries.</p>;
  }

  const today = entries.data?.local_date;
  const yesterday = today ? dayBefore(today) : null;
  const dayLabel = today ? formatHumanDate(today) : "Today";

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
  // Layout pattern: Material 3 "Small top app bar" + persistent tab strip.
  // The date heading sits in flow and scrolls away naturally. Only the
  // tab strip pins, at constant height — no scroll-event JS, no
  // competing animations, no flicker. The `-mx-4 md:-mx-8` bleed lets
  // the glass strip extend to the AppShell content frame edges.
  //
  // PullToRefresh and SwipeNavigator both apply `transform` to their
  // wrapper (motion.div with drag/y), and a transformed ancestor
  // breaks `position: sticky` — descendants stop sticking relative to
  // the viewport and instead anchor inside the transformed box. So
  // the sticky tab strip MUST be a sibling of those wrappers, not a
  // descendant. The wrappers stay scoped to the scrolling tab content
  // below.
  const tabContent = (
    <>
      <TabsContent value="manual" className="mt-6">
        <ManualBody
          dailyInput={dailyInput.data?.input ?? null}
          tags={dailyInput.data?.tags ?? []}
          onSaveDaily={(body) => saveDaily.mutate(body)}
          isSavingDaily={saveDaily.isPending}
        />
      </TabsContent>

      <TabsContent value="chat" className="mt-0">
        <ChatPanel
          headerHidden={headerHidden}
          onHeaderHiddenChange={setHeaderHidden}
        />
      </TabsContent>

      <TabsContent value="talk" className="mt-6">
        <Card>
          <CardContent className="px-6 py-10 text-sm text-muted-foreground">
            Voice mode is coming in the next phase. For now, use Chat or Manual.
          </CardContent>
        </Card>
      </TabsContent>
    </>
  );

  return (
    <Tabs value={mode} onValueChange={handleModeChange}>
      {/* Auto-hide-on-scroll-down: max-height + opacity collapse when
       *  ChatPanel reports a downward scroll. Transitioning max-height
       *  rather than display so the tab strip below slides up smoothly
       *  and the chat panel's `top` (which depends on headerHidden) can
       *  animate in lockstep. */}
      <header
        aria-hidden={headerHidden}
        className={cn(
          "flex flex-wrap items-baseline justify-between gap-x-3 gap-y-2 overflow-hidden",
          "transition-[max-height,opacity,margin] duration-200 ease-out",
          headerHidden
            ? "pointer-events-none mb-0 max-h-0 opacity-0"
            : "mb-4 max-h-[8rem] opacity-100",
        )}
      >
        <div className="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1">
          <h1 className="font-serif text-h1">{dayLabel}</h1>
          {me.data ? (
            <p className="font-mono text-xs tabular-nums text-muted-foreground">
              <span>{currentTime}</span> · {me.data.timezone} · new day at{" "}
              {minutesToHHMM(me.data.day_start_minutes)}
            </p>
          ) : null}
        </div>

        {extractionStatus === "pending" || extractionStatus === "running" ? (
          <span
            role="status"
            aria-live="polite"
            className="inline-flex shrink-0 items-center gap-1.5 rounded-full bg-accent/15 px-3 py-1 text-xs font-medium text-accent"
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
            className="inline-flex shrink-0 items-center gap-1.5 rounded-full border border-destructive/40 bg-destructive/10 px-3 py-1 text-xs font-medium text-destructive transition-colors hover:bg-destructive/15 disabled:opacity-60"
          >
            <AlertCircle className="h-3 w-3" aria-hidden />
            {finalizeRetry.isPending ? "Retrying…" : "Retry update"}
          </button>
        ) : null}
      </header>

      <div
        // Top offset is the AppShell mobile header's actual rendered
        // height (set as a CSS var by AppShell's ResizeObserver).
        // Resolves to 0 on desktop where the mobile header is
        // `md:hidden`, so the bar pins to viewport top there.
        style={{ top: "var(--app-mobile-header-h, 0px)" }}
        className="sticky z-20 -mx-4 md:-mx-8 px-4 md:px-8 py-2 bg-background/85 backdrop-blur-md border-b border-border/60"
      >
        <TabsList className="grid h-9 w-full grid-cols-3 md:inline-flex md:w-auto">
          <TabsTrigger value="manual">Manual</TabsTrigger>
          <TabsTrigger value="chat">Chat</TabsTrigger>
          <TabsTrigger value="talk" disabled className="gap-1.5">
            Talk
            <span className="rounded-full bg-muted-foreground/15 px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">
              soon
            </span>
          </TabsTrigger>
        </TabsList>
      </div>

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
            {tabContent}
          </SwipeNavigator>
        ) : (
          tabContent
        )}
      </PullToRefresh>
    </Tabs>
  );
}

interface ManualBodyProps {
  dailyInput: DailyInput | null;
  tags: TagDayLink[];
  onSaveDaily: (body: DailyInputUpsertBody) => void;
  isSavingDaily: boolean;
}


// ManualBody — Energy Audit's five-prompt template wrapped in DailyInputs.
// The card stack flow + per-question slots from Phase 6a are retired
// here: the fixed template is small enough that one card with five
// fields beats a slot-by-slot walkthrough.
function ManualBody({ dailyInput, tags, onSaveDaily, isSavingDaily }: ManualBodyProps) {
  return (
    <div className="space-y-6">
      <DailyInputs
        input={dailyInput}
        tags={tags}
        onSave={onSaveDaily}
        isSaving={isSavingDaily}
      />
      <GoalCheckInBlock />
    </div>
  );
}
