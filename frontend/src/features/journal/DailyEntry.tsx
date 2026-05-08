import { useCallback, useEffect, useState } from "react";
import { AlertCircle, Loader2 } from "lucide-react";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { CardShell, CardStack, type CardSlotApi, type CardStackSlot } from "@/components/ui/card-stack";
import { PaperPage, PaperPageBlock } from "@/components/ui/paper-page";
import { Slider } from "@/components/ui/slider";
import { Textarea } from "@/components/ui/textarea";
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
import { useEntries, useQuestions, useSaveEntry, ENTRY_DATES_KEY, entriesKey } from "./hooks";
import { DailyInputs } from "@/features/daily/DailyInputs";
import type { DailyInput } from "@/features/daily/api";
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
          questions={questions.data ?? []}
          entryByQuestion={entryByQuestion}
          dailyInput={dailyInput.data?.input ?? null}
          onSaveDaily={(body) => saveDaily.mutate(body)}
          isSavingDaily={saveDaily.isPending}
          dayLabel={dayLabel}
        />
      </TabsContent>

      <TabsContent value="chat" className="mt-6">
        <ChatPanel />
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
      <header className="mb-4 flex flex-wrap items-baseline justify-between gap-x-3 gap-y-2">
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
  questions: Question[];
  entryByQuestion: Map<string, JournalEntry>;
  dailyInput: DailyInput | null;
  onSaveDaily: (body: { mood_score: number | null; emotions_text: string; notes: string }) => void;
  isSavingDaily: boolean;
  dayLabel: string;
}

// "all touched" = every question has an entry row AND mood/emotions have
// been set. Save mutation deletes rows on empty body, so a present entry
// implies non-empty content.
function allAnswered(
  qs: Question[],
  byQ: Map<string, JournalEntry>,
  daily: DailyInput | null,
): boolean {
  const dailyTouched = !!daily?.mood_score || !!(daily?.emotions_text ?? "").trim();
  if (qs.length === 0) return dailyTouched;
  return dailyTouched && qs.every((q) => byQ.has(q.id));
}

function ManualBody({
  questions,
  entryByQuestion,
  dailyInput,
  onSaveDaily,
  isSavingDaily,
  dayLabel,
}: ManualBodyProps) {
  const save = useSaveEntry();
  const reduceMotion = useReducedMotion();

  // "complete" = filling done OR user clicked "Show full page" / advanced
  // past the last card. We don't auto-flip back to filling on edits —
  // once you're on the paper page, you stay there.
  const initialComplete = allAnswered(questions, entryByQuestion, dailyInput);
  const [reachedEnd, setReachedEnd] = useState(initialComplete);
  const phase: "filling" | "complete" =
    initialComplete || reachedEnd ? "complete" : "filling";

  const handleBlockSave = (questionId: string, body: string) => {
    save.mutate({ questionId, body });
  };

  // Slots: Mood → Emotions → ...questions. Mood/Emotions write back
  // through the existing daily-input mutation; questions through
  // useSaveEntry. Each slot sees `hasValue` so the stack lands on the
  // first thing that needs attention.
  const slots: CardStackSlot[] = [
    {
      id: "__mood",
      hasValue: dailyInput?.mood_score != null,
      render: (api) => (
        <MoodSlot
          api={api}
          dailyInput={dailyInput}
          onSaveDaily={onSaveDaily}
        />
      ),
    },
    {
      id: "__emotions",
      hasValue: !!(dailyInput?.emotions_text ?? "").trim(),
      render: (api) => (
        <EmotionsSlot
          api={api}
          dailyInput={dailyInput}
          onSaveDaily={onSaveDaily}
        />
      ),
    },
    ...questions.map((q) => ({
      id: q.id,
      hasValue: entryByQuestion.has(q.id),
      render: (api: CardSlotApi) => (
        <QuestionSlot
          api={api}
          question={q}
          initialBody={entryByQuestion.get(q.id)?.body ?? ""}
          onSubmit={(body) => save.mutate({ questionId: q.id, body })}
        />
      ),
    })),
  ];

  return (
    <div className="space-y-6">
      {questions.length === 0 ? (
        <>
          <DailyInputs
            input={dailyInput}
            onSave={onSaveDaily}
            isSaving={isSavingDaily}
          />
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
        </>
      ) : (
        // Q7 morph: AnimatePresence keyed on `phase`. CardStack exits
        // opacity+y; PaperPage enters opacity+y; serif date settles
        // letter-spacing as a small celebratory micro-moment. Reduced-
        // motion path is opacity-only.
        <AnimatePresence mode="wait" initial={false}>
          {phase === "filling" ? (
            <motion.div
              key="filling"
              initial={false}
              exit={
                reduceMotion
                  ? { opacity: 0, transition: { duration: 0.16 } }
                  : { opacity: 0, y: -12, transition: { duration: 0.2, ease: [0.4, 0, 1, 1] } }
              }
            >
              <CardStack slots={slots} onComplete={() => setReachedEnd(true)} />
            </motion.div>
          ) : (
            <motion.div
              key="complete"
              initial={
                reduceMotion
                  ? { opacity: 0 }
                  : { opacity: 0, y: 8 }
              }
              animate={
                reduceMotion
                  ? { opacity: 1, transition: { duration: 0.18 } }
                  : { opacity: 1, y: 0, transition: { duration: 0.36, ease: [0.2, 0, 0, 1] } }
              }
              className="space-y-6"
            >
              <DailyInputs
                input={dailyInput}
                onSave={onSaveDaily}
                isSaving={isSavingDaily}
              />
              <PaperPage
                eyebrow="Today"
                title={
                  <motion.span
                    initial={
                      reduceMotion ? false : { letterSpacing: "-0.01em" }
                    }
                    animate={
                      reduceMotion ? false : { letterSpacing: "-0.03em" }
                    }
                    transition={{ duration: 0.36, ease: [0.2, 0, 0, 1] }}
                    style={{ display: "inline-block" }}
                  >
                    {dayLabel}
                  </motion.span>
                }
              >
                {questions.map((q) => (
                  <PaperPageBlock
                    key={q.id}
                    prompt={q.prompt}
                    initialBody={entryByQuestion.get(q.id)?.body ?? ""}
                    onSave={(body) => handleBlockSave(q.id, body)}
                    saving={save.isPending}
                  />
                ))}
              </PaperPage>
            </motion.div>
          )}
        </AnimatePresence>
      )}
    </div>
  );
}

// ---- Card slot components ----

interface SlotInputsProps {
  api: CardSlotApi;
  dailyInput: DailyInput | null;
  onSaveDaily: (body: { mood_score: number | null; emotions_text: string; notes: string }) => void;
}

function MoodSlot({ api, dailyInput, onSaveDaily }: SlotInputsProps) {
  const [value, setValue] = useState<number | null>(dailyInput?.mood_score ?? null);
  const handleAdvance = () => {
    onSaveDaily({
      mood_score: value,
      emotions_text: dailyInput?.emotions_text ?? "",
      notes: dailyInput?.notes ?? "",
    });
    api.advance();
  };
  const display = value ?? 5;
  const isSet = value !== null;
  return (
    <CardShell
      api={{ ...api, advance: handleAdvance }}
      eyebrow="Check-in"
      prompt="How are you feeling today?"
      footerHint="1 = low · 10 = great · skip leaves it unset"
    >
      <div className="flex flex-1 flex-col items-center justify-center gap-6">
        <div className="flex items-baseline gap-3">
          <span className="font-serif text-display tabular-nums">
            {isSet ? display : "—"}
          </span>
          <span className="text-base text-muted-foreground">/ 10</span>
        </div>
        <div className="w-full max-w-md">
          <Slider
            value={[display]}
            min={1}
            max={10}
            step={1}
            ticks
            onValueChange={([v]) => setValue(v)}
          />
        </div>
        {!isSet ? (
          <button
            type="button"
            onClick={() => setValue(5)}
            className="text-xs text-accent underline-offset-4 hover:underline"
          >
            Tap to set
          </button>
        ) : (
          <button
            type="button"
            onClick={() => setValue(null)}
            className="text-xs text-muted-foreground underline-offset-4 hover:underline"
          >
            Clear
          </button>
        )}
      </div>
    </CardShell>
  );
}

function EmotionsSlot({ api, dailyInput, onSaveDaily }: SlotInputsProps) {
  const [text, setText] = useState<string>(dailyInput?.emotions_text ?? "");
  const handleAdvance = () => {
    onSaveDaily({
      mood_score: dailyInput?.mood_score ?? null,
      emotions_text: text,
      notes: dailyInput?.notes ?? "",
    });
    api.advance();
  };
  return (
    <CardShell
      api={{ ...api, advance: handleAdvance }}
      eyebrow="Check-in"
      prompt="What emotions are you feeling?"
      footerHint="Free text — anxious, relieved, grateful… ⌘↵ to advance"
    >
      <Textarea
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
            e.preventDefault();
            handleAdvance();
          }
        }}
        autoFocus
        placeholder="Anxious before the meeting, then relieved when it went well…"
        rows={6}
        className={cn(
          "flex-1 resize-none border-transparent bg-transparent px-0 leading-prose text-body",
          "focus-visible:ring-0 focus-visible:ring-offset-0",
          "focus-visible:border-b-border focus-visible:border-b rounded-none",
        )}
      />
    </CardShell>
  );
}

interface QuestionSlotProps {
  api: CardSlotApi;
  question: Question;
  initialBody: string;
  onSubmit: (body: string) => void;
}

function QuestionSlot({ api, question, initialBody, onSubmit }: QuestionSlotProps) {
  const [draft, setDraft] = useState(initialBody);
  const handleAdvance = () => {
    onSubmit(draft);
    api.advance();
  };
  return (
    <CardShell
      api={{ ...api, advance: handleAdvance }}
      prompt={question.prompt}
      footerHint="⌘↵ to submit · empty answers are allowed"
    >
      <Textarea
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
            e.preventDefault();
            handleAdvance();
          }
        }}
        autoFocus
        placeholder="Write whatever comes to mind…"
        rows={6}
        className={cn(
          "flex-1 resize-none border-transparent bg-transparent px-0 leading-prose text-body",
          "focus-visible:ring-0 focus-visible:ring-offset-0",
          "focus-visible:border-b-border focus-visible:border-b rounded-none",
        )}
      />
    </CardShell>
  );
}
