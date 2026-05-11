import { cn } from "@/lib/utils";

interface BrandMarkProps {
  size?: number;
  className?: string;
}

// Fold mark — a page with a dog-eared corner. Matches Logo.html concept 04:
// thin outline, 38% corner cut diagonally, two flat triangles (terracotta on
// top, page-colored underneath). No crease stroke — the color split *is* the
// fold. The page outline inherits currentColor; the dog-ear is hard-tinted to
// --primary because that slot holds the brand's warm accent across palettes
// (--accent is teal in the default ember palette).
export function BrandMark({ size = 24, className }: BrandMarkProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden="true"
      className={cn("text-foreground", className)}
    >
      <rect
        x="1"
        y="1"
        width="22"
        height="22"
        rx="2"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.4"
      />
      <path d="M14.9 0 L24 0 L24 9.1 Z" className="fill-primary" />
      <path d="M14.9 0 L24 9.1 L14.9 9.1 Z" className="fill-background" />
    </svg>
  );
}
