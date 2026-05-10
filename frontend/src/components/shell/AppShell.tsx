import { useCallback, useEffect, useRef, useState } from "react";
import {
  Navigate,
  Outlet,
  useLocation,
} from "react-router-dom";
import { Menu, LogOut } from "lucide-react";
import { useMutation, useQueryClient } from "@tanstack/react-query";

import { logout } from "@/features/auth/api";
import { ME_KEY, useMe } from "@/features/auth/useAuth";
import {
  ENTRY_DATES_KEY,
  QUESTIONS_KEY,
  entriesKey,
  useEntries,
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
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet";
import { cn } from "@/lib/utils";

import { Sidebar } from "./Sidebar";
import { JournalDateBlock } from "./JournalDateBlock";
import { NavMenu } from "./NavMenu";
import { AppShellSkeleton } from "./AppShellSkeleton";

export function AppShell() {
  const me = useMe();
  const qc = useQueryClient();
  const location = useLocation();
  const [drawerOpen, setDrawerOpen] = useState(false);

  // Close the drawer whenever route changes (e.g. tap a NavLink inside
  // the drawer). Keying on pathname keeps it simple — every navigation
  // commit dismisses the sheet.
  useEffect(() => {
    setDrawerOpen(false);
  }, [location.pathname]);

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
    // Stats panel powers /summary; warm it so the page paints from
    // cache when the user navigates there. Cheap GET — one ~6KB JSON.
    qc.prefetchQuery({
      queryKey: STATS_KEY(90),
      queryFn: () => getStats(90),
      staleTime: 60_000,
    });
    // Today's check-in lands above the questions on /; prefetch
    // alongside entries to keep the cold-start waterfall flat.
    qc.prefetchQuery({
      queryKey: dailyInputKey(),
      queryFn: () => getDailyInput(),
      staleTime: 30_000,
    });
    // Phase 6a: chat is the default mode of /. Prefetching the
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
  // sibling sticky bars (DailyEntry's tab strip, ChatPanel's coverage
  // strip) can pin flush against it without guessing. On desktop the
  // header is `md:hidden` and offsetHeight is 0, so the var resolves to
  // 0px and the consumers naturally pin to viewport top there.
  //
  // Uses a callback ref instead of useRef + useEffect: AppShell renders
  // <AppShellSkeleton /> while `me` is pending, so the real <header>
  // mounts later. A `[]`-deps effect would fire once with a null ref
  // and never re-run, leaving the var unset.
  const observerRef = useRef<ResizeObserver | null>(null);
  const resizeListenerRef = useRef<(() => void) | null>(null);
  const headerRef = useCallback((el: HTMLElement | null) => {
    if (observerRef.current) {
      observerRef.current.disconnect();
      observerRef.current = null;
    }
    if (resizeListenerRef.current) {
      window.removeEventListener("resize", resizeListenerRef.current);
      resizeListenerRef.current = null;
    }
    if (!el) {
      document.documentElement.style.removeProperty("--app-mobile-header-h");
      return;
    }
    const update = () => {
      document.documentElement.style.setProperty(
        "--app-mobile-header-h",
        `${el.offsetHeight}px`,
      );
    };
    update();
    const ro = new ResizeObserver(update);
    ro.observe(el);
    observerRef.current = ro;
    window.addEventListener("resize", update);
    resizeListenerRef.current = update;
  }, []);
  useEffect(() => {
    return () => {
      observerRef.current?.disconnect();
      if (resizeListenerRef.current) {
        window.removeEventListener("resize", resizeListenerRef.current);
      }
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
  // First-run gate: a freshly-verified user with no onboarded_at gets
  // funneled to the walkthrough instead of /today. The /onboarding route
  // owns the inverse gate (already-onboarded → /today), so a hard reload
  // mid-flow keeps working.
  if (!me.data.onboarded_at) {
    return <Navigate to="/onboarding" replace />;
  }

  return (
    <div className="flex min-h-svh bg-background text-foreground">
      <Sidebar
        user={me.data}
        onSignOut={() => signOut.mutate()}
        signingOut={signOut.isPending}
      />
      <div className="flex min-h-svh min-w-0 flex-1 flex-col">
        <header
          ref={headerRef}
          className="sticky top-0 z-30 flex items-center justify-between border-b border-border/60 bg-background/80 px-2 backdrop-blur-md md:hidden pt-[env(safe-area-inset-top)]"
        >
          <Sheet open={drawerOpen} onOpenChange={setDrawerOpen}>
            <SheetTrigger asChild>
              <button
                type="button"
                aria-label="Open menu"
                className={cn(
                  "inline-flex h-10 w-10 items-center justify-center rounded-md",
                  "text-muted-foreground hover:bg-secondary hover:text-foreground",
                  "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 ring-offset-background",
                )}
              >
                <Menu className="h-5 w-5" />
              </button>
            </SheetTrigger>
            <MobileDrawerContent
              user={me.data}
              onSignOut={() => signOut.mutate()}
              signingOut={signOut.isPending}
            />
          </Sheet>
          <ThemeToggle />
        </header>
        <main className="flex-1 pb-12">
          <div className="mx-auto w-full max-w-3xl px-4 py-6 md:px-8 md:py-10">
            <Outlet />
          </div>
        </main>
      </div>
      <Toaster />
    </div>
  );
}

interface MobileDrawerContentProps {
  user: ReturnType<typeof useMe>["data"];
  onSignOut: () => void;
  signingOut: boolean;
}

function MobileDrawerContent({ user, onSignOut, signingOut }: MobileDrawerContentProps) {
  const entries = useEntries();
  const journalDate = entries.data?.local_date ?? null;

  return (
    <SheetContent side="left" className="p-0">
      <SheetHeader className="pt-5 pb-1">
        <SheetTitle>Journaling Guru</SheetTitle>
      </SheetHeader>
      <div className="px-4 pb-2">
        <JournalDateBlock journalDate={journalDate} />
      </div>
      <div className="flex flex-1 flex-col px-3">
        <NavMenu layoutId="drawer-nav-active" />
      </div>
      <div className="border-t border-border/60 p-3 space-y-2">
        {user ? (
          <p className="truncate px-2 text-xs text-muted-foreground" title={user.email}>
            {user.email}
          </p>
        ) : null}
        <div className="flex items-center justify-between gap-2 px-1">
          <ThemeToggle />
          <button
            type="button"
            onClick={onSignOut}
            disabled={signingOut}
            aria-label="Sign out"
            title="Sign out"
            className={cn(
              "inline-flex h-9 items-center gap-2 rounded-md px-2.5 text-xs",
              "text-muted-foreground hover:bg-secondary hover:text-foreground",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 ring-offset-background",
              "disabled:opacity-50",
            )}
          >
            <LogOut className="h-3.5 w-3.5" />
            {signingOut ? "Signing out…" : "Sign out"}
          </button>
        </div>
      </div>
    </SheetContent>
  );
}
