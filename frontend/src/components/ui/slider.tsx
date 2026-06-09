import * as React from "react";
import * as SliderPrimitive from "@radix-ui/react-slider";

import { cn } from "@/lib/utils";

// Slider — Radix primitive with discrete tick marks. Used by the
// mood input on Today (1..10, step 1, single thumb). Visual:
//
//   ●——————————————————●——————●
//   1  2  3  4  5  6  7  8  9  10
//
// Range + thumb use --primary (ink) — burnt orange on ember. Accent is
// reserved for small margin-pen flourishes elsewhere.

interface Props
  extends React.ComponentPropsWithoutRef<typeof SliderPrimitive.Root> {
  ticks?: boolean;
}

export const Slider = React.forwardRef<
  React.ElementRef<typeof SliderPrimitive.Root>,
  Props
>(({ className, ticks = false, min, max, step, ...props }, ref) => {
  const tickValues =
    ticks && typeof min === "number" && typeof max === "number" && typeof step === "number"
      ? Array.from({ length: Math.floor((max - min) / step) + 1 }, (_, i) => min + i * step)
      : [];
  return (
    <div className={cn("relative", className)}>
      <SliderPrimitive.Root
        ref={ref}
        min={min}
        max={max}
        step={step}
        className={cn(
          "relative flex w-full touch-none select-none items-center",
          "h-9",
        )}
        {...props}
      >
        <SliderPrimitive.Track className="relative h-1.5 w-full grow overflow-hidden rounded-full bg-secondary">
          <SliderPrimitive.Range className="absolute h-full bg-primary" />
        </SliderPrimitive.Track>
        <SliderPrimitive.Thumb
          className={cn(
            "block h-5 w-5 rounded-full border-2 border-primary bg-card shadow-sm",
            "ring-offset-background transition-transform",
            "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2",
            "hover:scale-110 focus-visible:scale-110",
            "disabled:pointer-events-none disabled:opacity-50",
          )}
          aria-label="Mood"
        />
      </SliderPrimitive.Root>
      {tickValues.length > 0 ? (
        <div className="mt-1 flex justify-between px-2 text-[10px] font-mono text-muted-foreground tabular-nums">
          {tickValues.map((v) => (
            <span key={v}>{v}</span>
          ))}
        </div>
      ) : null}
    </div>
  );
});
Slider.displayName = "Slider";
