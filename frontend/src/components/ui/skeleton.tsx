import * as React from "react";

import { cn } from "@/lib/utils";

// CSS-only shimmer. Pure overlay so it works in both themes (the keyframe
// is theme-agnostic; the gradient uses muted-foreground at low alpha).
export function Skeleton({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        "relative overflow-hidden rounded-md bg-muted",
        "before:absolute before:inset-0 before:-translate-x-full before:animate-shimmer",
        "before:bg-gradient-to-r before:from-transparent before:via-foreground/[0.06] before:to-transparent",
        "motion-reduce:before:animate-none",
        className,
      )}
      aria-hidden="true"
      {...props}
    />
  );
}
