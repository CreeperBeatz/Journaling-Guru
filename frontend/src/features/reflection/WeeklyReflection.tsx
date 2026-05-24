import { useEffect, useState } from "react";
import { Navigate, useSearchParams } from "react-router-dom";
import { Sparkles } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

import { useMe } from "@/features/auth/useAuth";
import { ReflectionResponse } from "./api";
import {
  usePatchReflection,
  useStartReflection,
  useThisWeekReflection,
} from "./hooks";
import { LetterCard } from "./cards/LetterCard";
import { WeeklyChat } from "./WeeklyChat";
import { WeeklySummary } from "./WeeklySummary";

type WeeklyTab = "summary" | "reflection";

const STORAGE_KEY = "journai.weeklyTab";

// WeeklyReflection — /weekly. State machine:
//
//   1. Not started → IdleScreen ("Start reflection (5–10 min)").
//   2. Started, step=1 → LetterReadingView (the letter on its own;
//      "Continue to reflection" advances to step 2).
//   3. Started, step≥2 (and after wrap-up) → Tabs view (Summary +
//      Reflection chat). Default lands on Reflection on first entry,
//      remembered after that.
//
// Replay (from the Summary tab) clears completed_at + sets step=1, so
// the page re-renders into the LetterReadingView. The chat transcript
// is preserved (replay doesn't wipe the chat session).
export function WeeklyReflection() {
  const me = useMe();
  const isReflectionDay =
    me.data != null &&
    typeof me.data.local_weekday === "number" &&
    me.data.local_weekday === me.data.reflection_weekday;
  const reflection = useThisWeekReflection();

  if (me.isPending) {
    return (
      <Card>
        <CardContent className="px-6 py-8 text-sm text-muted-foreground">
          Loading the week…
        </CardContent>
      </Card>
    );
  }
  if (me.data != null && !isReflectionDay) {
    return <Navigate to="/" replace />;
  }
  if (reflection.isPending) {
    return (
      <Card>
        <CardContent className="px-6 py-8 text-sm text-muted-foreground">
          Loading the week…
        </CardContent>
      </Card>
    );
  }
  if (reflection.isError) {
    return (
      <Card>
        <CardContent className="px-6 py-8 text-sm text-destructive">
          {reflection.error.message}
        </CardContent>
      </Card>
    );
  }

  const data = reflection.data!;
  if (!data.started) {
    return <IdleScreen data={data} />;
  }
  if (data.step <= 1 && !data.completed_at) {
    return <LetterReadingView data={data} />;
  }
  return <TabsView data={data} />;
}

// ---------------- IdleScreen ----------------

function IdleScreen({ data }: { data: ReflectionResponse }) {
  const start = useStartReflection();

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <p className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground">
          Weekly reflection
        </p>
        <h1 className="font-serif text-h1">This week, looking back</h1>
        <p className="text-sm text-muted-foreground">
          {data.week_start} → {data.week_end} · {data.entry_count}{" "}
          {data.entry_count === 1 ? "logged day" : "logged days"}
        </p>
      </header>

      <Card>
        <CardContent className="space-y-4 px-6 py-10 text-center">
          <p className="text-sm text-muted-foreground">
            Journaling Guru has written a letter to you, summarizing how
            your week looked like. You can then choose to reflect on your
            week, and set something small to change for the next one.
            Takes about 10 minutes of focused time.
          </p>
          <Button
            size="lg"
            onClick={() => start.mutate()}
            disabled={start.isPending}
            className="gap-2"
          >
            <Sparkles className="h-4 w-4" />
            {start.isPending ? "Starting…" : "Start weekly reflection"}
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}

// ---------------- LetterReadingView ----------------

function LetterReadingView({ data }: { data: ReflectionResponse }) {
  const patch = usePatchReflection();
  const [, setParams] = useSearchParams();

  const advance = async () => {
    try {
      await patch.mutateAsync({ step: 2 });
    } catch {
      /* toast surfaced upstream */
      return;
    }
    // Force the Reflection tab on first landing — the user just clicked
    // "Continue to reflection", so a previously remembered Summary tab
    // shouldn't override that intent. Also persist to localStorage so
    // re-visits stick to Reflection until they explicitly switch.
    try {
      localStorage.setItem(STORAGE_KEY, "reflection");
    } catch {
      /* ignore */
    }
    setParams(
      (p) => {
        p.set("tab", "reflection");
        return p;
      },
      { replace: true },
    );
  };

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <p className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground">
          Weekly reflection
        </p>
        <h1 className="font-serif text-h1">Your letter</h1>
        <p className="text-sm text-muted-foreground">
          {data.week_start} → {data.week_end} · read it, then we'll talk it
          through
        </p>
      </header>

      <LetterCard data={data} saving={patch.isPending} onContinue={advance} />
    </div>
  );
}

// ---------------- TabsView ----------------

function TabsView({ data }: { data: ReflectionResponse }) {
  const [params, setParams] = useSearchParams();
  const urlTab = (params.get("tab") as WeeklyTab | null) ?? null;
  const [tab, setTab] = useState<WeeklyTab>(() => {
    if (urlTab === "summary" || urlTab === "reflection") return urlTab;
    try {
      const stored = localStorage.getItem(STORAGE_KEY);
      if (stored === "summary") return "summary";
    } catch {
      /* ignore */
    }
    return "reflection";
  });

  useEffect(() => {
    if (urlTab && urlTab !== tab) setTab(urlTab);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [urlTab]);

  const onTabChange = (value: string) => {
    const next: WeeklyTab = value === "summary" ? "summary" : "reflection";
    setTab(next);
    try {
      localStorage.setItem(STORAGE_KEY, next);
    } catch {
      /* ignore */
    }
    setParams(
      (p) => {
        p.set("tab", next);
        return p;
      },
      { replace: true },
    );
  };

  return (
    <Tabs value={tab} onValueChange={onTabChange}>
      <div
        style={{ top: "var(--app-mobile-header-h, 0px)" }}
        className="sticky z-20 -mx-4 -mt-6 md:-mx-8 md:-mt-10 px-4 md:px-8 py-2 bg-background/85 backdrop-blur-md border-b border-border/60"
      >
        <TabsList className="grid w-full grid-cols-2">
          <TabsTrigger value="summary">Summary</TabsTrigger>
          <TabsTrigger value="reflection">Reflection</TabsTrigger>
        </TabsList>
      </div>

      <TabsContent value="summary" className="mt-6">
        <WeeklySummary data={data} />
      </TabsContent>

      <TabsContent value="reflection" className="mt-0">
        <WeeklyChat onFinished={() => onTabChange("summary")} />
      </TabsContent>
    </Tabs>
  );
}
