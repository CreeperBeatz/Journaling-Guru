import { createBrowserRouter } from "react-router-dom";

import { App } from "./App";
import { AuthGate, GuestOnly } from "./features/auth/AuthGate";
import { MagicLinkRequest } from "./features/auth/MagicLinkRequest";
import { MagicLinkVerify } from "./features/auth/MagicLinkVerify";
import { SignedInHome } from "./features/auth/SignedInHome";
import { HealthPage } from "./features/health/HealthPage";

// Phase 2 surface: sign-in / verify / signed-in landing. The real journal
// routes (today, history, summaries, voice, settings) get added in
// later phases on top of AuthGate.
export const router = createBrowserRouter([
  {
    path: "/",
    element: <App />,
    children: [
      {
        index: true,
        element: (
          <AuthGate>
            <SignedInHome />
          </AuthGate>
        ),
      },
      {
        path: "auth/login",
        element: (
          <GuestOnly>
            <MagicLinkRequest />
          </GuestOnly>
        ),
      },
      { path: "auth/verify", element: <MagicLinkVerify /> },
      // Diagnostic page — public, hits /api/version.
      { path: "health", element: <HealthPage /> },
    ],
  },
]);
