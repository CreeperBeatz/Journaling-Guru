import { useEffect } from "react";
import { Sparkles } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";

import { ReflectionResponse } from "./api";
import {
  useCompleteReflection,
  usePatchReflection,
  useStartReflection,
} from "./hooks";
import { PatternAndSurpriseCard } from "./cards/PatternAndSurpriseCard";
import { GoalReviewCard } from "./cards/GoalReviewCard";
import { ShapeNextCard } from "./cards/ShapeNextCard";
import { DonePage } from "./cards/DonePage";

interface Props {
  data: ReflectionResponse;
}

// WeeklyWizard — orchestrates Idle → Card1 → Card2 → Card3 → Done.
// State is server-driven via `data.started`, `data.step`, and
// `data.completed_at`. The wizard nudges step forward via PATCH on
// each Continue, and flips to Done via POST /complete.
export function WeeklyWizard({ data }: Props) {
  // Done view wins — once completed, render frozen summary.
  if (data.completed_at) {
    return <DonePage data={data} completedAt={data.completed_at} />;
  }

  if (!data.started) {
    return <IdleScreen data={data} />;
  }

  return <ActiveWizard data={data} />;
}

// ---------------- Idle ----------------

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
            Walk through the week — pattern, goals, and what's next.
            About 5 minutes.
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

// ---------------- Active ----------------

function ActiveWizard({ data }: { data: ReflectionResponse }) {
  const patch = usePatchReflection();
  const complete = useCompleteReflection();

  // If the user is on Card 2 but has no active goals, auto-skip to Card 3.
  useEffect(() => {
    if (data.step === 2 && data.active_goals.length === 0) {
      patch.mutate({ step: 3 });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data.step, data.active_goals.length]);

  const advance = (next: number) => {
    patch.mutate({ step: next });
  };

  const finish = () => {
    complete.mutate();
  };

  const totalSteps = data.active_goals.length === 0 ? 2 : 3;
  const visibleStep = data.active_goals.length === 0 && data.step >= 2
    ? data.step - 1
    : data.step;

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <p className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground">
          Weekly reflection · step {visibleStep} of {totalSteps}
        </p>
        <h1 className="font-serif text-h1">This week, looking back</h1>
        <p className="text-sm text-muted-foreground">
          {data.week_start} → {data.week_end}
        </p>
      </header>

      {data.step === 1 ? (
        <PatternAndSurpriseCard
          data={data}
          saving={patch.isPending}
          onContinue={() => advance(2)}
        />
      ) : null}

      {data.step === 2 ? (
        <GoalReviewCard
          data={data}
          saving={patch.isPending}
          onContinue={() => advance(3)}
        />
      ) : null}

      {data.step === 3 ? (
        <ShapeNextCard onDone={finish} />
      ) : null}
    </div>
  );
}
