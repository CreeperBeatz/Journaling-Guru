import { useDailyInput } from "@/features/daily/hooks";

import { WeeklyReflection } from "./WeeklyReflection";

// /weekly — standalone host for WeeklyReflection. Previously this view
// hijacked /today on the user's reflection_weekday; now it has its
// own route and a sidebar entry, navigated via the Weekly nav button.
export function WeeklyReflectionPage() {
  const dailyInput = useDailyInput();

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <p className="font-mono text-[11px] uppercase tracking-[0.08em] text-muted-foreground">
          Weekly
        </p>
        <h1 className="font-serif text-h1">This week</h1>
      </header>
      <WeeklyReflection
        dailyInput={dailyInput.data?.input ?? null}
        tags={dailyInput.data?.tags ?? []}
      />
    </div>
  );
}
