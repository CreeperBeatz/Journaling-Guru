import { Navigate, useNavigate } from "react-router-dom";
import { Sparkles } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";

import { ReflectionResponse } from "./api";
import { useStartReflection } from "./hooks";
import { DonePage } from "./cards/DonePage";

interface Props {
  data: ReflectionResponse;
}

// WeeklyWizard — Idle → ReflectionChat → Done. The chat itself opens
// with the letter as its first assistant message (seeded server-side
// in CreateOrResumeWeekly), so there's no separate "read the letter"
// step anymore. WeeklyWizard's only inline view is the IdleScreen;
// everything else lives at /weekly/chat or in DonePage.
export function WeeklyWizard({ data }: Props) {
  if (data.completed_at) {
    return <DonePage data={data} completedAt={data.completed_at} />;
  }
  if (!data.started) {
    return <IdleScreen data={data} />;
  }
  return <Navigate to="/weekly/chat" replace />;
}

// ---------------- Idle ----------------

function IdleScreen({ data }: { data: ReflectionResponse }) {
  const start = useStartReflection();
  const navigate = useNavigate();

  const letterReady =
    data.letter.trim() !== "" ||
    data.charged.trim() !== "" ||
    data.drained.trim() !== "" ||
    data.grateful.trim() !== "" ||
    data.insights.trim() !== "";

  const onStart = async () => {
    try {
      await start.mutateAsync();
      navigate("/weekly/chat");
    } catch {
      /* toast surfaced upstream */
    }
  };

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
            Read your letter and talk it through. About 10 minutes.
          </p>
          <Button
            size="lg"
            onClick={onStart}
            disabled={start.isPending || !letterReady}
            className="gap-2"
          >
            <Sparkles className="h-4 w-4" />
            {start.isPending
              ? "Starting…"
              : letterReady
                ? "Start weekly reflection"
                : "Letter on its way…"}
          </Button>
          {!letterReady ? (
            <p className="text-xs text-muted-foreground">
              The letter is still being written. It'll be ready in a moment.
            </p>
          ) : null}
        </CardContent>
      </Card>
    </div>
  );
}
