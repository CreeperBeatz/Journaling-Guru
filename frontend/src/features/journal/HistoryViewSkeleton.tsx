import { Skeleton } from "@/components/ui/skeleton";

export function HistoryViewSkeleton() {
  return (
    <div className="space-y-6">
      <header className="space-y-2">
        <Skeleton className="h-3 w-14" />
        <Skeleton className="h-9 w-48" />
        <Skeleton className="h-4 w-72" />
      </header>
      <div className="grid grid-cols-1 gap-6 md:grid-cols-[16rem,1fr]">
        <nav className="space-y-2">
          {[0, 1, 2, 3, 4, 5].map((i) => (
            <Skeleton key={i} className="h-14 w-full rounded-md" />
          ))}
        </nav>
        <Skeleton className="h-48 w-full rounded-xl" />
      </div>
    </div>
  );
}
