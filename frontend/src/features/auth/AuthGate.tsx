import { ReactNode } from "react";
import { Navigate } from "react-router-dom";

import { useMe } from "./useAuth";

// AppShell handles the auth check + AppShellSkeleton for protected routes
// (see components/shell/AppShell.tsx). GuestOnly stays here as the inverse
// guard for /auth/login and similar surfaces — it must redirect away if
// the user already has a session.
export function GuestOnly({ children }: { children: ReactNode }) {
  const me = useMe();
  if (me.isPending) return null;
  if (me.data) return <Navigate to="/" replace />;
  return <>{children}</>;
}
