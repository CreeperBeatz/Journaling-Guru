import { cn } from "@/lib/utils";

export interface StatTile {
  label: string;
  value: string;
  hint?: string;
}

export interface StatTilesProps {
  tiles: StatTile[];
  className?: string;
}

/**
 * 3-up tiles for "stats this week" — streak / days reflected / avg
 * mood. Tabular-nums for crisp alignment; small caption eyebrow under
 * each value. The dashboard composes the values; this component is
 * purely presentational.
 */
export function StatTiles({ tiles, className }: StatTilesProps) {
  return (
    <div
      className={cn(
        "grid gap-4",
        tiles.length === 3 ? "grid-cols-3" : "grid-cols-2",
        className,
      )}
    >
      {tiles.map((t) => (
        <div key={t.label} className="space-y-1 text-center">
          <div className="font-mono text-2xl tabular-nums">{t.value}</div>
          <div className="font-mono text-[10px] uppercase tracking-[0.08em] text-muted-foreground">
            {t.label}
          </div>
          {t.hint ? (
            <div className="text-[11px] text-muted-foreground/80">{t.hint}</div>
          ) : null}
        </div>
      ))}
    </div>
  );
}
