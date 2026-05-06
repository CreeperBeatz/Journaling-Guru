import { Skeleton } from "@/components/ui/skeleton";

// Shape-aware fallback for DailyEntry — header + 3 question cards.
export function DailyEntrySkeleton() {
  return (
    <div className="space-y-6">
      <header className="space-y-2">
        <Skeleton className="h-3 w-12" />
        <Skeleton className="h-9 w-72" />
        <Skeleton className="h-4 w-56" />
      </header>
      <div className="space-y-8">
        {[0, 1, 2].map((i) => (
          <section key={i} className="space-y-2">
            <div className="flex items-center justify-between">
              <Skeleton className="h-5 w-64" />
              <Skeleton className="h-5 w-16 rounded-full" />
            </div>
            <Skeleton className="h-32 w-full" />
          </section>
        ))}
      </div>
    </div>
  );
}
