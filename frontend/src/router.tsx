import { lazy, Suspense, type ReactNode } from "react";
import { Navigate, createBrowserRouter } from "react-router-dom";

import { App } from "./App";
import { AuthLayout } from "./components/shell/AuthLayout";
import { GuestOnly } from "./features/auth/AuthGate";
import { AuthCardSkeleton } from "./features/auth/AuthCardSkeleton";
import { DailyEntrySkeleton } from "./features/journal/DailyEntrySkeleton";
import { HistoryViewSkeleton } from "./features/journal/HistoryViewSkeleton";
import { SettingsSkeleton } from "./features/settings/SettingsSkeleton";
import {
  SummariesPageSkeleton,
  SummaryDetailSkeleton,
} from "./features/summaries/SummariesPageSkeleton";

// Lazy-loaded route components — each ships in its own chunk.
const DailyEntry = lazy(() =>
  import("./features/journal/DailyEntry").then((m) => ({ default: m.DailyEntry })),
);
const HistoryView = lazy(() =>
  import("./features/journal/HistoryView").then((m) => ({ default: m.HistoryView })),
);
const Settings = lazy(() =>
  import("./features/settings/Settings").then((m) => ({ default: m.Settings })),
);
const SummaryPage = lazy(() =>
  import("./features/summary/SummaryPage").then((m) => ({ default: m.SummaryPage })),
);
const SummaryDetail = lazy(() =>
  import("./features/summaries/SummaryDetail").then((m) => ({ default: m.SummaryDetail })),
);
const MagicLinkRequest = lazy(() =>
  import("./features/auth/MagicLinkRequest").then((m) => ({ default: m.MagicLinkRequest })),
);
const MagicLinkVerify = lazy(() =>
  import("./features/auth/MagicLinkVerify").then((m) => ({ default: m.MagicLinkVerify })),
);
const HealthPage = lazy(() =>
  import("./features/health/HealthPage").then((m) => ({ default: m.HealthPage })),
);
const ResourcesPage = lazy(() =>
  import("./features/resources/ResourcesPage").then((m) => ({ default: m.ResourcesPage })),
);

function withSuspense(node: ReactNode, fallback: ReactNode) {
  return <Suspense fallback={fallback}>{node}</Suspense>;
}

// Two top-level layouts:
//   - <App /> (AppShell) wraps protected routes — sidebar/bottom-tab chrome.
//     AppShell handles the /api/me gate (redirect to login on no session)
//     so route children don't need a per-route AuthGate.
//   - <AuthLayout /> wraps un-authed surfaces — minimal centered card, no nav.
//
// Suspense fallbacks live at the route level so /api/me invalidation
// doesn't flash skeletons over already-painted content.
export const router = createBrowserRouter([
  {
    element: <AuthLayout />,
    children: [
      {
        path: "/auth/login",
        element: (
          <GuestOnly>
            {withSuspense(<MagicLinkRequest />, <AuthCardSkeleton />)}
          </GuestOnly>
        ),
      },
      {
        path: "/auth/verify",
        element: withSuspense(<MagicLinkVerify />, <AuthCardSkeleton />),
      },
      // Diagnostic page — public, hits /api/version.
      {
        path: "/health",
        element: withSuspense(<HealthPage />, <AuthCardSkeleton />),
      },
      // Crisis resources — public so a user in distress can reach it
      // from anywhere, including the in-chat CrisisCard.
      {
        path: "/resources",
        element: withSuspense(<ResourcesPage />, <AuthCardSkeleton />),
      },
    ],
  },
  {
    path: "/",
    element: <App />,
    children: [
      { index: true, element: withSuspense(<DailyEntry />, <DailyEntrySkeleton />) },
      { path: "today", element: <Navigate to="/" replace /> },
      { path: "history", element: withSuspense(<HistoryView />, <HistoryViewSkeleton />) },
      { path: "history/:date", element: withSuspense(<HistoryView />, <HistoryViewSkeleton />) },
      // Phase 4.1: Questions moved into Settings as a tab. Old bookmarks
      // redirect to /settings?tab=questions so nothing 404s.
      { path: "questions", element: <Navigate to="/settings?tab=questions" replace /> },
      { path: "settings", element: withSuspense(<Settings />, <SettingsSkeleton />) },
      // IA rename: /summaries → /summary (Trends + ByQuestion tabs).
      // Direct deep links to a specific summary id keep working via
      // the legacy SummaryDetail component until the dashboard widgets
      // (step 6) replace it.
      { path: "summary", element: withSuspense(<SummaryPage />, <SummariesPageSkeleton />) },
      { path: "summary/:id", element: withSuspense(<SummaryDetail />, <SummaryDetailSkeleton />) },
      { path: "summaries", element: <Navigate to="/summary" replace /> },
      { path: "summaries/:id", element: <Navigate to="/summary" replace /> },
    ],
  },
]);
