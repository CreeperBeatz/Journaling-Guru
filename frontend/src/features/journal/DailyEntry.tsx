import { useCallback, useEffect, useState } from "react";
import { AlertCircle, Loader2 } from "lucide-react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";

import { PullToRefresh } from "@/components/shell/PullToRefresh";
import { SwipeNavigator } from "@/components/shell/SwipeNavigator";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useMe } from "@/features/auth/useAuth";
import { ChatPanel } from "@/features/chat/ChatPanel";
import { VoicePanel } from "@/features/chat/VoicePanel";
import {
  isExtractionInFlight,
  useExtractionStatus,
  useFinalizeChat,
  useTodayChatSession,
} from "@/features/chat/hooks";

import { listEntries } from "./api";
import { useEntries, ENTRY_DATES_KEY, entriesKey } from "./hooks";
import { DailyInputs } from "@/features/daily/DailyInputs";
import type { DailyInput, DailyInputUpsertBody, TagDayLink } from "@/features/daily/api";
import { useDailyInput, useSaveDailyInput } from "@/features/daily/hooks";
import { GoalCheckInBlock } from "@/features/goals/GoalCheckInBlock";
import { WeeklyReflection } from "@/features/reflection/WeeklyReflection";

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

// weekdayOf returns 0..6 (Sun..Sat) for a YYYY-MM-DD string. The
// local_date is a wall-clock day, so we parse in UTC space and read
// getUTCDay — this avoids any timezone drift.
function weekdayOf(yyyymmdd: string): number | null {
  const [y, m, d] = yyyymmdd.split("-").map(Number);
  if (!y || !m || !d) return null;
  return new Date(Date.UTC(y, m - 1, d)).getUTCDay();
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
  const navigate = useNavigate();
  const qc = useQueryClient();

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
      if (!isMode(next)) return;
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
    // Retry path stays manual-wins — same intent as the original click.
    finalizeRetry.mutate({ sessionId: chatSession.id });
  };

  if (entries.isPending) {
    return <p className="text-sm text-muted-foreground">Loading…</p>;
  }
  if (entries.isError) {
    return <p className="text-sm text-destructive">Couldn't load today's entries.</p>;
  }

  const today = entries.data?.local_date;
  const yesterday = today ? dayBefore(today) : null;

  // Phase 7 — Weekly reflection. On the user's chosen reflection_weekday
  // we replace the daily Manual/Chat tabs with the reflection flow. The
  // user can opt out by passing ?mode=manual or ?mode=chat in the URL
  // (and a "switch to today's check-in" link is rendered inline).
  const todayWeekday = today ? weekdayOf(today) : null;
  const onReflectionDay =
    !!me.data && todayWeekday !== null && todayWeekday === me.data.reflection_weekday;
  const userOverride = isMode(urlMode) && urlMode !== "talk";
  if (onReflectionDay && !userOverride) {
    return (
      <div className="space-y-6">
        <WeeklyReflection
          dailyInput={dailyInput.data?.input ?? null}
          tags={dailyInput.data?.tags ?? []}
        />
        <button
          type="button"
          onClick={() => handleModeChange("manual")}
          className="text-xs text-muted-foreground underline-offset-2 hover:underline"
        >
          Switch to today&apos;s daily check-in instead →
        </button>
      </div>
    );
  }

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
        <ChatPanel />
      </TabsContent>

      <TabsContent value="talk" className="mt-6">
        <VoicePanel />
      </TabsContent>
    </>
  );

  const statusPill =
    extractionStatus === "pending" || extractionStatus === "running" ? (
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
    ) : null;

  return (
    <Tabs value={mode} onValueChange={handleModeChange}>
      <div
        // Top offset is the AppShell mobile header's actual rendered
        // height (set as a CSS var by AppShell's ResizeObserver).
        // Resolves to 0 on desktop where the mobile header is
        // `md:hidden`, so the bar pins to viewport top there.
        style={{ top: "var(--app-mobile-header-h, 0px)" }}
        className="sticky z-20 -mx-4 -mt-6 md:-mx-8 md:-mt-10 px-4 md:px-8 py-2 bg-background/85 backdrop-blur-md border-b border-border/60"
      >
        <div className="flex flex-col gap-2">
          {statusPill ? <div className="self-end">{statusPill}</div> : null}
          <TabsList className="grid w-full grid-cols-3">
            <TabsTrigger value="manual">Manual</TabsTrigger>
            <TabsTrigger value="chat">Chat</TabsTrigger>
            <TabsTrigger value="talk">Talk</TabsTrigger>
          </TabsList>
        </div>
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
