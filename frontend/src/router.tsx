import { lazy, Suspense, type ReactNode } from "react";
import { createBrowserRouter } from "react-router-dom";

import { App } from "./App";
import { AuthLayout } from "./components/shell/AuthLayout";
import { GuestOnly } from "./features/auth/AuthGate";
import { AuthCardSkeleton } from "./features/auth/AuthCardSkeleton";
import { DailyEntrySkeleton } from "./features/journal/DailyEntrySkeleton";
import { HistoryViewSkeleton } from "./features/journal/HistoryViewSkeleton";
import { QuestionEditorSkeleton } from "./features/journal/QuestionEditorSkeleton";
import { SettingsSkeleton } from "./features/settings/SettingsSkeleton";

// Lazy-loaded route components — each ships in its own chunk.
const DailyEntry = lazy(() =>
  import("./features/journal/DailyEntry").then((m) => ({ default: m.DailyEntry })),
);
const HistoryView = lazy(() =>
  import("./features/journal/HistoryView").then((m) => ({ default: m.HistoryView })),
);
const QuestionEditor = lazy(() =>
  import("./features/journal/QuestionEditor").then((m) => ({ default: m.QuestionEditor })),
);
const Settings = lazy(() =>
  import("./features/settings/Settings").then((m) => ({ default: m.Settings })),
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
    ],
  },
  {
    path: "/",
    element: <App />,
    children: [
      { index: true, element: withSuspense(<DailyEntry />, <DailyEntrySkeleton />) },
      { path: "history", element: withSuspense(<HistoryView />, <HistoryViewSkeleton />) },
      { path: "history/:date", element: withSuspense(<HistoryView />, <HistoryViewSkeleton />) },
      { path: "questions", element: withSuspense(<QuestionEditor />, <QuestionEditorSkeleton />) },
      { path: "settings", element: withSuspense(<Settings />, <SettingsSkeleton />) },
    ],
  },
]);
