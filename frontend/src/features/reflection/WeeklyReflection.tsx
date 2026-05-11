import { useQuery } from "@tanstack/react-query";
import { Navigate } from "react-router-dom";

import { Card, CardContent } from "@/components/ui/card";

import { ApiError } from "@/api/client";
import { useMe } from "@/features/auth/useAuth";
import { ReflectionResponse, getThisWeekReflection } from "./api";
import { REFLECTION_THIS_WEEK_KEY } from "./hooks";
import { WeeklyWizard } from "./WeeklyWizard";

// WeeklyReflection — top-level view at /weekly. Loads this week's
// reflection state and delegates to WeeklyWizard, which handles the
// Idle / Active (Cards 1-3) / Done branching. Gated to the user's
// reflection_weekday — off-day visits bounce to /.
export function WeeklyReflection() {
  const me = useMe();
  const isReflectionDay =
    me.data != null &&
    typeof me.data.local_weekday === "number" &&
    me.data.local_weekday === me.data.reflection_weekday;

  const reflection = useQuery<ReflectionResponse, ApiError>({
    queryKey: REFLECTION_THIS_WEEK_KEY,
    queryFn: getThisWeekReflection,
    staleTime: 60_000,
    enabled: isReflectionDay,
  });

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

  return <WeeklyWizard data={reflection.data!} />;
}
