import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";

import { Zone1Card, Zone2Card, Zone3Card } from "./Zones";
import {
  useSummaryZone1,
  useSummaryZone2,
  useSummaryZone3,
} from "./hooks";

// /summary — three-zone vertical stack under the Energy Audit pivot.
//
//   Zone 1 — At a glance:    sparkline, 7d-vs-prior delta, headline,
//                            active goal status.
//   Zone 2 — What's driving: top drainers + top chargers (last 30d)
//                            with avg-mood and a low-confidence badge
//                            for tags under the spec's 7-appearance
//                            threshold.
//   Zone 3 — What I tried:   goals ledger (active + historical).
//
// Replaces the prior Trends + By Question tabs surface entirely. Each
// zone is its own query so a single failure doesn't blank the page.
export function SummaryPage() {
  const zone1 = useSummaryZone1();
  const zone2 = useSummaryZone2(30);
  const zone3 = useSummaryZone3();

  return (
    <div className="space-y-6">
      <header className="space-y-1">
        <h1 className="font-serif text-h1">Patterns</h1>
      </header>

      {zone1.isPending ? (
        <Zone1Skeleton />
      ) : zone1.isError ? (
        <ErrorCard message={zone1.error.message} />
      ) : zone1.data ? (
        <Zone1Card data={zone1.data} />
      ) : null}

      {zone2.isPending ? (
        <Zone2Skeleton />
      ) : zone2.isError ? (
        <ErrorCard message={zone2.error.message} />
      ) : zone2.data ? (
        <Zone2Card data={zone2.data} />
      ) : null}

      {zone3.isPending ? (
        <Zone3Skeleton />
      ) : zone3.isError ? (
        <ErrorCard message={zone3.error.message} />
      ) : zone3.data ? (
        <Zone3Card data={zone3.data} />
      ) : null}
    </div>
  );
}

// Shape-aware skeletons — each echoes its zone card's layout so the
// pending → loaded swap doesn't shift the page.

// Zone 1: sparkline + 7d stat, headline box, one goal row.
function Zone1Skeleton() {
  return (
    <Card>
      <CardHeader className="pb-3">
        <Skeleton className="h-5 w-24" />
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-3">
          <div className="min-w-0 flex-1 space-y-1">
            <Skeleton className="h-3 w-32" />
            <Skeleton className="h-12 w-full max-w-md" />
          </div>
          <div className="shrink-0 space-y-1">
            <Skeleton className="h-3 w-16" />
            <Skeleton className="h-8 w-14" />
          </div>
        </div>
        <Skeleton className="h-16 w-full" />
        <Skeleton className="h-14 w-full" />
      </CardContent>
    </Card>
  );
}

// Zone 2: two tag tables side by side.
function Zone2Skeleton() {
  return (
    <Card>
      <CardHeader className="space-y-2 pb-3">
        <Skeleton className="h-5 w-32" />
        <Skeleton className="h-3 w-64" />
      </CardHeader>
      <CardContent className="grid gap-6 md:grid-cols-2">
        {[0, 1].map((col) => (
          <div key={col} className="space-y-2">
            <Skeleton className="h-3 w-20" />
            {[0, 1, 2].map((row) => (
              <div key={row} className="flex items-baseline justify-between gap-3 pb-1">
                <Skeleton className="h-4 w-28" />
                <Skeleton className="h-3 w-20" />
              </div>
            ))}
          </div>
        ))}
      </CardContent>
    </Card>
  );
}

// Zone 3: goals ledger rows.
function Zone3Skeleton() {
  return (
    <Card>
      <CardHeader className="pb-3">
        <Skeleton className="h-5 w-28" />
      </CardHeader>
      <CardContent className="space-y-3">
        {[0, 1].map((i) => (
          <Skeleton key={i} className="h-14 w-full" />
        ))}
      </CardContent>
    </Card>
  );
}

function ErrorCard({ message }: { message: string }) {
  return (
    <Card>
      <CardContent className="px-6 py-8 text-sm text-destructive">
        {message}
      </CardContent>
    </Card>
  );
}
