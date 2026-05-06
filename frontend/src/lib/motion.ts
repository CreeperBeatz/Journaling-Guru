import type { Transition, Variants } from "motion/react";

// Easing curves codified once so components don't drift. Pulled from the
// design language doc (frontend/DESIGN.md §3) — change here, change there.
export const easeStandard = [0.32, 0.72, 0, 1] as const;
export const easeEmphasized = [0.2, 0, 0, 1] as const;
export const easeExit = [0.4, 0, 1, 1] as const;

export const springTactile: Transition = {
  type: "spring",
  stiffness: 380,
  damping: 30,
};

// Page enter/exit — wrap <Outlet /> in <AnimatePresence mode="wait">
// keyed on location.pathname.
export const pageVariants: Variants = {
  initial: { opacity: 0, y: 8 },
  animate: { opacity: 1, y: 0, transition: { duration: 0.28, ease: easeStandard } },
  exit: { opacity: 0, transition: { duration: 0.16, ease: easeExit } },
};

// List stagger — apply to a parent motion element with `variants={listContainer}`
// and each child with `variants={listItem}`. Cap visible count to 8 above
// the fold; render anything past that without animation.
export const listContainer: Variants = {
  initial: {},
  animate: {
    transition: { staggerChildren: 0.04, delayChildren: 0.05 },
  },
};

export const listItem: Variants = {
  initial: { opacity: 0, y: 6 },
  animate: { opacity: 1, y: 0, transition: { duration: 0.22, ease: easeStandard } },
};
