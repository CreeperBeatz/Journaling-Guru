import { ReactNode } from "react";
import { Navigate, useLocation } from "react-router-dom";

import { useMe } from "./useAuth";

// AuthGate is the route-level guard for everything that requires a
// signed-in user. While /api/me is loading we render a quiet placeholder
// rather than the children, so we don't briefly expose authed UI before
// realising the cookie is gone.
export function AuthGate({ children }: { children: ReactNode }) {
  const me = useMe();
  const location = useLocation();

  if (me.isPending) {
    return <p className="text-sm text-muted-foreground">Loading…</p>;
  }
  if (me.isError) {
    return (
      <p className="text-sm text-destructive">
        Couldn't reach the API. Check the backend is running.
      </p>
    );
  }
  if (!me.data) {
    return <Navigate to="/auth/login" replace state={{ from: location.pathname }} />;
  }
  return <>{children}</>;
}

// GuestOnly is the inverse: redirect the user away from /auth/login if
// they already have a working cookie.
export function GuestOnly({ children }: { children: ReactNode }) {
  const me = useMe();
  if (me.isPending) {
    return <p className="text-sm text-muted-foreground">Loading…</p>;
  }
  if (me.data) {
    return <Navigate to="/" replace />;
  }
  return <>{children}</>;
}
