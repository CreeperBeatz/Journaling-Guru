import { createBrowserRouter, Navigate } from "react-router-dom";

import { App } from "./App";
import { HealthPage } from "./features/health/HealthPage";

// Phase 1: just a health page so we can verify the wiring end-to-end.
// Auth, journal, voice, summaries, and push routes get added in their phases.
export const router = createBrowserRouter([
  {
    path: "/",
    element: <App />,
    children: [
      { index: true, element: <Navigate to="/health" replace /> },
      { path: "health", element: <HealthPage /> },
    ],
  },
]);
