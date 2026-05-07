import { ReactNode, useRef, useState } from "react";
import { motion, useMotionValue, useTransform, type PanInfo } from "motion/react";
import { Loader2, RefreshCw } from "lucide-react";

import { useIsTouch } from "@/lib/useMediaQuery";

interface Props {
  children: ReactNode;
  onRefresh: () => Promise<unknown> | void;
}

const TRIGGER = 80;
const MAX = 120;

// Native-feeling pull-to-refresh. Only activates when the page is already
// scrolled to the top — otherwise normal overscroll behavior wins. Touch
// devices only; desktop renders children verbatim.
export function PullToRefresh({ children, onRefresh }: Props) {
  const touch = useIsTouch();
  const [refreshing, setRefreshing] = useState(false);
  const y = useMotionValue(0);
  const startScroll = useRef(0);
  const armed = useRef(false);

  // Scale + rotation tied to drag distance. All hooks must be called
  // before the touch-only early return so the hook count is stable
  // across renders — otherwise React fires "Rendered more hooks than
  // during the previous render" the first time the media query flips.
  const indicatorOpacity = useTransform(y, [0, TRIGGER], [0, 1]);
  const indicatorRotate = useTransform(y, [0, MAX], [0, 360]);
  const indicatorY = useTransform(y, (v) => Math.max(0, v - 24));

  if (!touch) return <>{children}</>;

  const handleStart = () => {
    startScroll.current = window.scrollY;
    armed.current = startScroll.current === 0;
  };

  const handleEnd = async (_: unknown, info: PanInfo) => {
    if (!armed.current || refreshing) {
      y.set(0);
      return;
    }
    if (info.offset.y > TRIGGER) {
      setRefreshing(true);
      // Optional haptic — a no-op on browsers that don't expose it.
      navigator.vibrate?.(10);
      try {
        await onRefresh();
      } finally {
        setRefreshing(false);
        y.set(0);
      }
    } else {
      y.set(0);
    }
  };

  return (
    <div className="relative">
      <motion.div
        aria-hidden={!refreshing}
        className="pointer-events-none absolute left-1/2 top-0 z-10 -translate-x-1/2"
        style={{ y: indicatorY, opacity: indicatorOpacity }}
      >
        <div className="rounded-full border border-border bg-card p-2 shadow-md">
          {refreshing ? (
            <Loader2 className="h-4 w-4 animate-spin text-foreground" />
          ) : (
            <motion.div style={{ rotate: indicatorRotate }}>
              <RefreshCw className="h-4 w-4 text-foreground" />
            </motion.div>
          )}
        </div>
      </motion.div>
      <motion.div
        drag="y"
        dragConstraints={{ top: 0, bottom: 0 }}
        dragElastic={{ top: 0.4, bottom: 0 }}
        dragMomentum={false}
        style={{ y, touchAction: "pan-x pan-y" }}
        onDragStart={handleStart}
        onDragEnd={handleEnd}
      >
        {children}
      </motion.div>
    </div>
  );
}
