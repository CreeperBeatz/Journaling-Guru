import { Navigate, useLocation, useSearchParams } from "react-router-dom";

import { ThemeToggle } from "@/components/ui/theme-toggle";
import { Toaster } from "@/components/ui/sonner";
import { useMe } from "@/features/auth/useAuth";

import { OnboardingFlow } from "./OnboardingFlow";

// Sibling layout to <App /> — same warm-paper background as AuthLayout but
// with its own auth + onboarded gates so the route can live outside the
// app shell (no sidebar, no bottom-tabbar).
//
// Gate matrix:
//   me pending           → loader
//   me=null              → /auth/login (cookie expired)
//   me.onboarded_at set, no ?replay → /today
//   otherwise            → render the flow
export function OnboardingLayout() {
  const me = useMe();
  const location = useLocation();
  const [searchParams] = useSearchParams();
  const replay = searchParams.get("replay") === "1";

  if (me.isPending) {
    return (
      <div className="flex min-h-svh items-center justify-center bg-background text-sm text-muted-foreground">
        Loading…
      </div>
    );
  }
  if (me.isError) {
    return (
      <div className="flex min-h-svh items-center justify-center bg-background px-6 text-center">
        <p className="text-sm text-destructive">
          Couldn't reach the API. Check the backend is running.
        </p>
      </div>
    );
  }
  if (!me.data) {
    return (
      <Navigate
        to="/auth/login"
        replace
        state={{ from: location.pathname + location.search }}
      />
    );
  }
  if (me.data.onboarded_at && !replay) {
    return <Navigate to="/" replace />;
  }

  return (
    <div className="flex min-h-svh flex-col bg-background text-foreground">
      <header className="flex items-center justify-between px-6 pt-[max(env(safe-area-inset-top),1rem)] pb-2">
        <span className="font-serif italic text-xl tracking-tight">
          Journaling Guru
        </span>
        <ThemeToggle />
      </header>
      <main className="flex flex-1 items-start justify-center px-6 py-8 sm:items-center">
        <div className="w-full max-w-xl">
          <OnboardingFlow user={me.data} replay={replay} />
        </div>
      </main>
      <Toaster />
    </div>
  );
}
