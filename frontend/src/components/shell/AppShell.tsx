import { useEffect, useRef } from "react";
import {
  Navigate,
  NavLink,
  Outlet,
  useLocation,
} from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { logout } from "@/features/auth/api";
import { ME_KEY, useMe } from "@/features/auth/useAuth";
import {
  ENTRY_DATES_KEY,
  QUESTIONS_KEY,
  entriesKey,
} from "@/features/journal/hooks";
import {
  listEntries,
  listEntryDates,
  listQuestions,
} from "@/features/journal/api";
import { STATS_KEY } from "@/features/summaries/hooks";
import { getStats } from "@/features/summaries/api";
import { dailyInputKey } from "@/features/daily/hooks";
import { getDailyInput } from "@/features/daily/api";
import { chatSessionKey } from "@/features/chat/hooks";
import { getTodaySession } from "@/features/chat/api";
import { ThemeToggle } from "@/components/ui/theme-toggle";
import { Toaster } from "@/components/ui/sonner";

import { Sidebar } from "./Sidebar";
import { BottomTabBar } from "./BottomTabBar";
import { AppShellSkeleton } from "./AppShellSkeleton";

function pageTitle(pathname: string): string {
  if (pathname === "/") return "Today";
  if (pathname.startsWith("/history")) return "History";
  if (pathname.startsWith("/summaries")) return "Reflections";
  if (pathname.startsWith("/questions")) return "Questions";
  if (pathname.startsWith("/settings")) return "Settings";
  return "JournAI";
}

export function AppShell() {
  const me = useMe();
  const qc = useQueryClient();
  const location = useLocation();

  // Once /api/me lands, fan out prefetches in parallel — kills the cold
  // waterfall (me → render → questions/entries). Both queries are warm
  // (or in-flight) by the time DailyEntry's chunk resolves.
  useEffect(() => {
    if (!me.data) return;
    qc.prefetchQuery({
      queryKey: QUESTIONS_KEY,
      queryFn: async () => (await listQuestions()).questions,
      staleTime: 5 * 60_000,
    });
    qc.prefetchQuery({
      queryKey: entriesKey(),
      queryFn: () => listEntries(),
      staleTime: 30_000,
    });
    qc.prefetchQuery({
      queryKey: ENTRY_DATES_KEY,
      queryFn: async () => (await listEntryDates(180)).dates,
      staleTime: 60_000,
    });
    // Stats panel powers /summaries; warm it so the page paints from
    // cache when the user navigates there. Cheap GET — one ~6KB JSON.
    qc.prefetchQuery({
      queryKey: STATS_KEY(90),
      queryFn: () => getStats(90),
      staleTime: 60_000,
    });
    // Today's check-in lands above the questions on /today; prefetch
    // alongside entries to keep the cold-start waterfall flat.
    qc.prefetchQuery({
      queryKey: dailyInputKey(),
      queryFn: () => getDailyInput(),
      staleTime: 30_000,
    });
    // Phase 6a: chat is the default mode of /today. Prefetching the
    // session envelope means the streamed greeting can start the moment
    // the route's chunk resolves, instead of after a round-trip.
    qc.prefetchQuery({
      queryKey: chatSessionKey(),
      queryFn: () => getTodaySession(),
      staleTime: 60_000,
    });
  }, [me.data?.id, qc]);

  const signOut = useMutation({
    mutationFn: () => logout(),
    onSettled: () => qc.invalidateQueries({ queryKey: ME_KEY }),
  });

  // Publish the mobile header's actual rendered height as a CSS var so
  // sibling sticky bars (DailyEntry's Today bar, ChatPanel's coverage
  // strip) can pin flush against it without guessing. On desktop the
  // header is `md:hidden` and offsetHeight is 0, so the var resolves to
  // 0px and the consumers naturally pin to viewport top there.
  const headerRef = useRef<HTMLElement>(null);
  useEffect(() => {
    const el = headerRef.current;
    if (!el) return;
    const update = () => {
      document.documentElement.style.setProperty(
        "--app-mobile-header-h",
        `${el.offsetHeight}px`,
      );
    };
    update();
    const ro = new ResizeObserver(update);
    ro.observe(el);
    window.addEventListener("resize", update);
    return () => {
      ro.disconnect();
      window.removeEventListener("resize", update);
    };
  }, []);

  if (me.isPending) {
    return <AppShellSkeleton />;
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

  return (
    <div className="flex min-h-svh bg-background text-foreground">
      <Sidebar
        user={me.data}
        onSignOut={() => signOut.mutate()}
        signingOut={signOut.isPending}
      />
      <div className="flex min-h-svh flex-1 flex-col">
        <header
          ref={headerRef}
          className="sticky top-0 z-30 flex items-center justify-between border-b border-border/60 bg-background/80 px-4 backdrop-blur-md md:hidden pt-[env(safe-area-inset-top)]"
        >
          <NavLink to="/" className="font-serif italic text-lg leading-none py-3">
            JournAI
          </NavLink>
          <span className="text-sm font-medium text-muted-foreground">
            {pageTitle(location.pathname)}
          </span>
          <ThemeToggle />
        </header>
        <main className="flex-1 pb-20 md:pb-12">
          <div className="mx-auto w-full max-w-3xl px-4 py-6 md:px-8 md:py-10">
            <Outlet />
          </div>
        </main>
      </div>
      <BottomTabBar />
      <Toaster />
    </div>
  );
}
