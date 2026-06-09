import { useEffect, useRef, useState } from "react";
import {
  CheckCircle2,
  MoreHorizontal,
  RefreshCw,
  RotateCcw,
  Sparkles,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

import type { Zone1GoalStatus } from "@/features/summary/api";
import { useRegenerateSummary } from "@/features/summaries/hooks";

import { LIFE_DOMAINS, PatchReflectionBody, ReflectionResponse } from "./api";
import { MonthlyLetterCard } from "./cards/MonthlyLetterCard";
import { WeeklySynthesisCard } from "./cards/WeeklySynthesisCard";
import {
  usePatchReflection,
  useReplayReflection,
  useSetMonthlyIntention,
} from "./hooks";

interface Props {
  data: ReflectionResponse;
  /** Override the patch behavior — used by the History view to target
   * a past week's row. Defaults to this-week's PATCH. */
  onPatch?: (body: PatchReflectionBody) => void;
  /** Whether onPatch is currently in flight. */
  patchPending?: boolean;
  /** Hide the Replay button in History (replay only makes sense for
   * the current week). Defaults to true. */
  showReplay?: boolean;
  /** Hide the Regenerate menu in History — legacy letters aren't
   * regeneratable. Defaults to true. */
  showRegenerate?: boolean;
  /** Hide the header — used when WeeklySummary is embedded under a
   * page that already has its own header (e.g. History). */
  showHeader?: boolean;
}

// WeeklySummary — the "Summary" tab content on /weekly. Renders the
// letter, plus *editable* fields for the user's own touches: the
// surprise/continuity sentence and per-goal notes (manual-mode-style,
// save on blur via PATCH /api/reflection/this-week).
//
// Reused by the History view (HistoryWeeklyReflection) with `onPatch`
// pointed at the historical patch endpoint and showReplay=false.
export function WeeklySummary({
  data,
  onPatch,
  patchPending,
  showReplay = true,
  showRegenerate = true,
  showHeader = true,
}: Props) {
  const completedWhen = data.completed_at ? new Date(data.completed_at) : null;
  const newSet = new Set(data.new_goal_ids);
  const existingGoals = data.active_goals.filter((g) => !newSet.has(g.id));
  const freshGoals = data.active_goals.filter((g) => newSet.has(g.id));

  const thisWeekPatch = usePatchReflection();
  const replay = useReplayReflection();
  const regen = useRegenerateSummary();
  const patchFn = onPatch ?? ((body: PatchReflectionBody) => thisWeekPatch.mutate(body));
  const patching = onPatch ? !!patchPending : thisWeekPatch.isPending;

  // Mirror WeeklySynthesisCard's body-presence check so the regenerate
  // menu only surfaces when there's an existing letter to redo.
  const hasAnyBody =
    (data.charged?.trim() ?? "") !== "" ||
    (data.drained?.trim() ?? "") !== "" ||
    (data.grateful?.trim() ?? "") !== "" ||
    (data.insights?.trim() ?? "") !== "" ||
    (data.letter?.trim() ?? "") !== "";

  return (
    <div className="space-y-6">
      {showHeader ? (
        <header className="space-y-1">
          <p className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground">
            Weekly reflection
          </p>
          <h1 className="font-serif text-h1 inline-flex items-center gap-2">
            {completedWhen ? (
              <CheckCircle2 className="h-6 w-6 text-accent" />
            ) : null}
            This week
          </h1>
          <p className="text-sm text-muted-foreground">
            {data.week_start} → {data.week_end}
            {completedWhen ? ` · wrapped ${completedWhen.toLocaleString()}` : ""}
          </p>
        </header>
      ) : null}

      <WeeklySynthesisCard
        data={data}
        regenerating={regen.isPending}
        onRegenerate={
          showRegenerate
            ? () =>
                regen.mutate({
                  period_type: "week",
                  period_start: data.week_start,
                })
            : undefined
        }
      />

      {existingGoals.length > 0 ? (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="font-serif text-base">Active goals</CardTitle>
            <p className="text-xs italic text-muted-foreground">
              Why you set them — the words you'd want to re-hear.
            </p>
          </CardHeader>
          <CardContent className="space-y-2">
            {existingGoals.map((g) => (
              <GoalMotivationRow key={g.id} goal={g} />
            ))}
          </CardContent>
        </Card>
      ) : null}

      {freshGoals.length > 0 ? (
        <Card className="border-accent/40 bg-accent/5">
          <CardHeader className="pb-3">
            <CardTitle className="inline-flex items-center gap-2 font-serif text-base">
              <Sparkles className="h-4 w-4 text-accent" />
              New goals
            </CardTitle>
            <p className="text-xs italic text-muted-foreground">
              Shaped during this reflection.
            </p>
          </CardHeader>
          <CardContent className="space-y-2">
            {freshGoals.map((g) => (
              <GoalMotivationRow key={g.id} goal={g} fresh />
            ))}
          </CardContent>
        </Card>
      ) : null}

      <EditableSurpriseCard
        initialText={data.surprise_text}
        onSave={(text) => patchFn({ surprise_text: text })}
        saving={patching}
      />

      {data.monthly ? (
        <MonthSection
          monthly={data.monthly}
          editable={showReplay /* current week only — History is read-only */}
        />
      ) : null}

      {showReplay || (showRegenerate && hasAnyBody) ? (
        <div className="flex items-center justify-center gap-1 pt-2">
          {showReplay ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => replay.mutate()}
              disabled={replay.isPending}
              className="gap-1.5 text-xs text-muted-foreground hover:text-foreground"
            >
              <RotateCcw className="h-3.5 w-3.5" />
              {replay.isPending ? "Replaying…" : "Replay weekly reflection"}
            </Button>
          ) : null}
          {showRegenerate && hasAnyBody ? (
            <RegenerateMenu
              pending={regen.isPending}
              onRegenerate={() =>
                regen.mutate({
                  period_type: "week",
                  period_start: data.week_start,
                })
              }
            />
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

// MonthSection — the "This month" block on monthly weeks: the monthly
// letter, the intention (editable until the user is happy with it), the
// distilled direction note once the chat finalized, and a compact
// ratings row with deltas vs the previous rated month.
function MonthSection({
  monthly,
  editable,
}: {
  monthly: NonNullable<ReflectionResponse["monthly"]>;
  editable: boolean;
}) {
  const setIntention = useSetMonthlyIntention();
  const [intentionDraft, setIntentionDraft] = useState(monthly.intention_text);
  const [intentionDirty, setIntentionDirty] = useState(false);

  useEffect(() => {
    if (!intentionDirty) setIntentionDraft(monthly.intention_text);
  }, [monthly.intention_text, intentionDirty]);

  const saveIntention = () => {
    if (!intentionDirty) return;
    const text = intentionDraft.trim();
    if (text === "" || text === monthly.intention_text) {
      setIntentionDirty(false);
      setIntentionDraft(monthly.intention_text);
      return;
    }
    setIntention.mutate(text);
    setIntentionDirty(false);
  };

  const prev = monthly.prev_ratings ?? {};
  const ratingRows = monthly.ratings
    ? LIFE_DOMAINS.filter((d) => monthly.ratings![d.key] !== undefined).map((d) => {
        const score = monthly.ratings![d.key];
        const prior = prev[d.key];
        return { key: d.key, label: d.label, score, delta: prior !== undefined ? score - prior : null };
      })
    : [];

  return (
    <div className="space-y-6">
      <header className="space-y-1 pt-2">
        <p className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground">
          This month
        </p>
      </header>

      <MonthlyLetterCard monthly={monthly} />

      <Card className="border-accent/40 bg-accent/5">
        <CardHeader className="pb-3">
          <CardTitle className="font-serif text-base">
            Intention for next month
          </CardTitle>
          <p className="text-xs italic text-muted-foreground">
            One direction — broader than a goal.
          </p>
        </CardHeader>
        <CardContent>
          {editable ? (
            <Textarea
              value={intentionDraft}
              onChange={(e) => {
                setIntentionDraft(e.target.value);
                setIntentionDirty(true);
              }}
              onBlur={saveIntention}
              placeholder="e.g. Protect my mornings…"
              maxLength={300}
              rows={2}
              className="min-h-[3.5rem]"
              disabled={setIntention.isPending}
            />
          ) : (
            <p className="text-sm">
              {monthly.intention_text.trim() !== ""
                ? monthly.intention_text
                : "No intention was set this month."}
            </p>
          )}
        </CardContent>
      </Card>

      {monthly.direction_text.trim() !== "" ? (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="font-serif text-base">
              The direction check
            </CardTitle>
            <p className="text-xs italic text-muted-foreground">
              Where you said this is pointing — distilled from the conversation.
            </p>
          </CardHeader>
          <CardContent>
            <p className="whitespace-pre-wrap text-sm text-foreground/90">
              {monthly.direction_text}
            </p>
          </CardContent>
        </Card>
      ) : null}

      {ratingRows.length > 0 ? (
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="font-serif text-base">Life check-in</CardTitle>
            <p className="text-xs italic text-muted-foreground">
              0–10 satisfaction, with movement vs last month.
            </p>
          </CardHeader>
          <CardContent>
            <dl className="grid grid-cols-1 gap-x-6 gap-y-1.5 sm:grid-cols-2">
              {ratingRows.map((r) => (
                <div key={r.key} className="flex items-baseline justify-between gap-3">
                  <dt className="text-sm">{r.label}</dt>
                  <dd className="font-mono text-sm tabular-nums">
                    {r.score}
                    {r.delta !== null && r.delta !== 0 ? (
                      <span
                        className={cn(
                          "ml-1.5 text-[11px]",
                          r.delta > 0 ? "text-accent" : "text-destructive/80",
                        )}
                      >
                        {r.delta > 0 ? `+${r.delta}` : r.delta}
                      </span>
                    ) : null}
                  </dd>
                </div>
              ))}
            </dl>
          </CardContent>
        </Card>
      ) : null}
    </div>
  );
}

// RegenerateMenu — three-dot kebab with a single "Regenerate weekly
// message" item. Lives at the bottom of WeeklySummary so the destructive-
// looking option doesn't crowd the letter itself.
function RegenerateMenu({
  pending,
  onRegenerate,
}: {
  pending: boolean;
  onRegenerate: () => void;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onClick = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onClick);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onClick);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  return (
    <div ref={ref} className="relative">
      <Button
        type="button"
        variant="ghost"
        size="sm"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label="More actions"
        className="h-auto px-2 py-1.5 text-muted-foreground hover:text-foreground"
      >
        <MoreHorizontal className="h-4 w-4" />
      </Button>
      {open ? (
        <div
          role="menu"
          className="absolute bottom-full right-0 z-20 mb-1 min-w-[14rem] overflow-hidden rounded-md border border-border bg-popover py-1 text-sm shadow-md"
        >
          <button
            type="button"
            role="menuitem"
            onClick={() => {
              setOpen(false);
              onRegenerate();
            }}
            disabled={pending}
            className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-muted-foreground hover:bg-muted hover:text-foreground disabled:opacity-50"
          >
            <RefreshCw
              className={cn("h-3.5 w-3.5", pending && "animate-spin")}
            />
            {pending ? "Regenerating…" : "Regenerate weekly message"}
          </button>
        </div>
      ) : null}
    </div>
  );
}

// EditableSurpriseCard — the "thing to remember" field, save-on-blur.
function EditableSurpriseCard({
  initialText,
  onSave,
  saving,
}: {
  initialText: string;
  onSave: (text: string) => void;
  saving: boolean;
}) {
  const [text, setText] = useState(initialText);
  const [dirty, setDirty] = useState(false);

  // Reset local state when the server value changes from underneath us
  // (e.g. extraction sentence arrives after wrap-up). Only when we're
  // not actively editing.
  useEffect(() => {
    if (!dirty) setText(initialText);
  }, [initialText, dirty]);

  const handleBlur = () => {
    if (!dirty) return;
    if (text === initialText) {
      setDirty(false);
      return;
    }
    onSave(text);
    setDirty(false);
  };

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="font-serif text-base">
          The thing to remember
        </CardTitle>
        <p className="text-xs italic text-muted-foreground">
          One paragraph to remember.
        </p>
      </CardHeader>
      <CardContent>
        <Textarea
          value={text}
          onChange={(e) => {
            setText(e.target.value);
            setDirty(true);
          }}
          onBlur={handleBlur}
          placeholder="What you want next week to remember…"
          maxLength={1500}
          className="min-h-[14rem]"
          disabled={saving}
        />
      </CardContent>
    </Card>
  );
}

// GoalMotivationRow — read-only goal card surfacing the user's own
// reasons captured at creation (why_matters, if_followed,
// if_not_followed). The note textarea was removed in favour of this
// re-encounter view so the weekly reflection re-shows the user's
// motivation instead of asking for fresh commentary.
function GoalMotivationRow({
  goal: g,
  fresh,
}: {
  goal: Zone1GoalStatus;
  fresh?: boolean;
}) {
  const why = g.why_matters.trim();
  const ifFollowed = g.if_followed.trim();
  const ifNotFollowed = g.if_not_followed.trim();
  const hasMotivation = why || ifFollowed || ifNotFollowed;

  return (
    <div
      className={
        fresh
          ? "space-y-2 rounded-md border border-accent/40 bg-background/50 px-3 py-2"
          : "space-y-2 rounded-md border border-border/60 px-3 py-2"
      }
    >
      <div className="flex items-baseline justify-between gap-3">
        <p className="truncate text-sm font-medium">{g.title}</p>
        <p className="font-mono text-xs tabular-nums text-muted-foreground">
          {fresh ? "—" : `${g.kept_count}/${g.answered_count || 7}`}
        </p>
      </div>
      <p className="text-[11px] text-muted-foreground">
        {fresh
          ? `Starts ${g.start_date} · ends ${g.end_date}`
          : `Day ${g.day_index} of ${g.total_days} · ends ${g.end_date}`}
      </p>
      {hasMotivation ? (
        <dl className="space-y-1.5 pt-1 text-xs">
          {why ? (
            <div>
              <dt className="font-mono text-[10px] uppercase tracking-[0.08em] text-muted-foreground">
                Why it matters
              </dt>
              <dd className="text-foreground/90">{why}</dd>
            </div>
          ) : null}
          {ifFollowed ? (
            <div>
              <dt className="font-mono text-[10px] uppercase tracking-[0.08em] text-muted-foreground">
                If I follow it
              </dt>
              <dd className="text-foreground/90">{ifFollowed}</dd>
            </div>
          ) : null}
          {ifNotFollowed ? (
            <div>
              <dt className="font-mono text-[10px] uppercase tracking-[0.08em] text-muted-foreground">
                If I don't
              </dt>
              <dd className="text-foreground/90">{ifNotFollowed}</dd>
            </div>
          ) : null}
        </dl>
      ) : null}
    </div>
  );
}
