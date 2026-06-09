import { useEffect, useState } from "react";
import { useTheme } from "next-themes";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { Monitor, Moon, Sun } from "lucide-react";

import { cn } from "@/lib/utils";

const order = ["light", "dark", "system"] as const;
type ThemeChoice = (typeof order)[number];

const icon = {
  light: Sun,
  dark: Moon,
  system: Monitor,
};

const label = {
  light: "Light",
  dark: "Dark",
  system: "System",
};

interface Props {
  className?: string;
}

export function ThemeToggle({ className }: Props) {
  const { theme, setTheme } = useTheme();
  const reduce = useReducedMotion();
  const [mounted, setMounted] = useState(false);

  // next-themes resolves on the client; render a placeholder until then to
  // avoid a hydration mismatch on the icon.
  useEffect(() => setMounted(true), []);

  const current = (mounted ? (theme as ThemeChoice | undefined) : undefined) ?? "system";
  const Icon = icon[current];

  const cycle = () => {
    const idx = order.indexOf(current);
    setTheme(order[(idx + 1) % order.length]);
  };

  return (
    <button
      type="button"
      onClick={cycle}
      aria-label={`Theme: ${label[current]}. Click to cycle.`}
      title={`Theme: ${label[current]}`}
      className={cn(
        "inline-flex h-9 w-9 items-center justify-center rounded-md text-muted-foreground",
        "hover:bg-secondary hover:text-foreground",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 ring-offset-background",
        "transition-colors",
        className,
      )}
    >
      <AnimatePresence mode="wait" initial={false}>
        <motion.span
          key={current}
          initial={reduce ? { opacity: 0 } : { opacity: 0, rotate: -90, scale: 0.8 }}
          animate={reduce ? { opacity: 1 } : { opacity: 1, rotate: 0, scale: 1 }}
          exit={reduce ? { opacity: 0 } : { opacity: 0, rotate: 90, scale: 0.8 }}
          transition={{ duration: 0.18 }}
          className="inline-flex"
        >
          <Icon className="h-4 w-4" />
        </motion.span>
      </AnimatePresence>
    </button>
  );
}
