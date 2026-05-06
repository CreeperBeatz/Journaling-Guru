import { useQuery } from "@tanstack/react-query";

import { api, ApiError } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";

interface VersionResponse {
  version: string;
  phase: number;
}

// Phase 1 sanity page: hits /api/version, surfaces failure modes loudly.
// Replaced with the actual onboarding flow in Phase 2.
export function HealthPage() {
  const versionQuery = useQuery<VersionResponse, ApiError>({
    queryKey: ["version"],
    queryFn: () => api<VersionResponse>("/api/version"),
  });

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>Skeleton check</CardTitle>
          <CardDescription>
            Verifies that the React app, Vite proxy, and Go backend are all reachable.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {versionQuery.isPending && (
            <p className="text-sm text-muted-foreground">Pinging /api/version…</p>
          )}
          {versionQuery.isError && (
            <p className="text-sm text-destructive">
              Backend unreachable: {versionQuery.error.message}
            </p>
          )}
          {versionQuery.isSuccess && (
            <pre className="rounded-md bg-secondary p-3 text-sm">
              {JSON.stringify(versionQuery.data, null, 2)}
            </pre>
          )}
          <Button onClick={() => versionQuery.refetch()} variant="secondary">
            Re-check
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}
