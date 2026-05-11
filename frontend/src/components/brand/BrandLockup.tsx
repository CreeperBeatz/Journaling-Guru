import { cn } from "@/lib/utils";
import { BrandMark } from "./BrandMark";

interface BrandLockupProps {
  className?: string;
  markSize?: number;
}

// Lockup pairs the Fold mark with the italic-serif wordmark. The wrapper is a
// plain inline-flex span so callers can drop it inside a NavLink/Link/SheetTitle
// without nesting extra interactive elements.
export function BrandLockup({ className, markSize = 22 }: BrandLockupProps) {
  return (
    <span
      className={cn(
        // Instrument Serif italic — the wordmark style the app shipped with
        // before the Logo.html exploration. Keeps the mark + terracotta dot.
        "inline-flex items-center gap-2 font-serif italic text-xl tracking-tight leading-none",
        className,
      )}
    >
      <BrandMark size={markSize} className="shrink-0" />
      <span>
        journaling<span className="mx-[1px] not-italic text-primary" aria-hidden="true">.</span>guru
      </span>
    </span>
  );
}
