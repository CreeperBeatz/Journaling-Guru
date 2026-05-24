import { Card, CardContent } from "@/components/ui/card";

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
        <SkeletonCard label="Loading at-a-glance…" />
      ) : zone1.isError ? (
        <ErrorCard message={zone1.error.message} />
      ) : zone1.data ? (
        <Zone1Card data={zone1.data} />
      ) : null}

      {zone2.isPending ? (
        <SkeletonCard label="Loading drainers and chargers…" />
      ) : zone2.isError ? (
        <ErrorCard message={zone2.error.message} />
      ) : zone2.data ? (
        <Zone2Card data={zone2.data} />
      ) : null}

      {zone3.isPending ? (
        <SkeletonCard label="Loading goals…" />
      ) : zone3.isError ? (
        <ErrorCard message={zone3.error.message} />
      ) : zone3.data ? (
        <Zone3Card data={zone3.data} />
      ) : null}
    </div>
  );
}

function SkeletonCard({ label }: { label: string }) {
  return (
    <Card>
      <CardContent className="px-6 py-8 text-sm text-muted-foreground">
        {label}
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
