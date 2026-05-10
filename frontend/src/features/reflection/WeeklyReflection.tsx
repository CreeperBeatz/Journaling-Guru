import { useQuery } from "@tanstack/react-query";

import { Card, CardContent } from "@/components/ui/card";

import { ApiError } from "@/api/client";
import { ReflectionResponse, getThisWeekReflection } from "./api";
import { REFLECTION_THIS_WEEK_KEY } from "./hooks";
import { WeeklyWizard } from "./WeeklyWizard";

// WeeklyReflection — top-level view at /weekly. Loads this week's
// reflection state and delegates to WeeklyWizard, which handles the
// Idle / Active (Cards 1-3) / Done branching.
export function WeeklyReflection() {
  const reflection = useQuery<ReflectionResponse, ApiError>({
    queryKey: REFLECTION_THIS_WEEK_KEY,
    queryFn: getThisWeekReflection,
    staleTime: 60_000,
  });

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

  return <WeeklyWizard data={reflection.data!} />;
}
