import { Skeleton } from "@/components/ui/skeleton";

export function QuestionEditorSkeleton() {
  return (
    <div className="space-y-6">
      <header className="space-y-2">
        <Skeleton className="h-3 w-16" />
        <Skeleton className="h-9 w-56" />
        <Skeleton className="h-4 w-80" />
      </header>
      <Skeleton className="h-36 w-full rounded-xl" />
      <ul className="space-y-2">
        {[0, 1, 2, 3].map((i) => (
          <Skeleton key={i} className="h-16 w-full rounded-md" />
        ))}
      </ul>
    </div>
  );
}
