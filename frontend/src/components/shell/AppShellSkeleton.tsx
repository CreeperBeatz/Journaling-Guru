import { Loader2 } from "lucide-react";

// Generic placeholder shown when /api/me is in flight before the route's
// shape-aware skeleton can decide what to render. Kept deliberately quiet —
// no chrome, just a centered spinner — so a fast resolve doesn't flash a
// fake sidebar.
export function AppShellSkeleton() {
  return (
    <div className="flex min-h-svh items-center justify-center bg-background text-muted-foreground">
      <Loader2 className="h-5 w-5 animate-spin" />
    </div>
  );
}
