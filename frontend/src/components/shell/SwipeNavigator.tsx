import { ReactNode, useRef } from "react";
import { motion, useReducedMotion, type PanInfo } from "motion/react";

import { useIsTouch } from "@/lib/useMediaQuery";

interface Props {
  children: ReactNode;
  // Drag-right (positive offset) gesture. Conventionally "go back in time".
  onSwipeRight?: () => void;
  // Drag-left (negative offset). Conventionally "go forward in time".
  onSwipeLeft?: () => void;
  // Fired on drag start so adjacent dates can warm in cache.
  onDragStart?: () => void;
}

const TRIGGER_OFFSET = 80;
const TRIGGER_VELOCITY = 200;
// iOS Safari dead-zone: a drag starting in the leftmost 20px is the system
// back-swipe. Don't compete.
const IOS_DEAD_ZONE = 20;

// Wraps content with a horizontal drag gesture for date navigation.
// Renders children verbatim on non-touch devices — no JS overhead, no
// pointer-event hijacking.
export function SwipeNavigator({ children, onSwipeRight, onSwipeLeft, onDragStart }: Props) {
  const touch = useIsTouch();
  const reduce = useReducedMotion();
  const startX = useRef(0);

  if (!touch) return <>{children}</>;

  const handleDragEnd = (_: unknown, info: PanInfo) => {
    if (startX.current < IOS_DEAD_ZONE) return;
    const dx = info.offset.x;
    const vx = info.velocity.x;
    if (Math.abs(dx) < TRIGGER_OFFSET || Math.abs(vx) < TRIGGER_VELOCITY) return;
    if (dx > 0) onSwipeRight?.();
    else onSwipeLeft?.();
  };

  return (
    <motion.div
      drag={reduce ? false : "x"}
      dragConstraints={{ left: 0, right: 0 }}
      dragElastic={0.18}
      dragMomentum={false}
      onPointerDown={(e) => {
        startX.current = e.clientX;
      }}
      onDragStart={onDragStart}
      onDragEnd={handleDragEnd}
      // motion's `drag` prop adds will-change:transform; touch-action helps
      // the browser keep vertical scroll responsive while we capture x.
      style={{ touchAction: "pan-y" }}
    >
      {children}
    </motion.div>
  );
}
