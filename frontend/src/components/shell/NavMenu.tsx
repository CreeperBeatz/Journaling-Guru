import { NavLink } from "react-router-dom";
import {
  CalendarCheck,
  CalendarDays,
  MessageSquare,
  Settings as SettingsIcon,
  Sparkles,
  Target,
} from "lucide-react";
import { motion, useReducedMotion } from "motion/react";

import { cn } from "@/lib/utils";
import { useMe } from "@/features/auth/useAuth";

const baseNavItems = [
  // Weekly sits above Today as the primary-tinted CTA. Shown on the
  // user's reflection weekday, and on later days while that week's
  // reflection is still pending (carry-over) — see filter below.
  { to: "/weekly", end: false, label: "Weekly", icon: CalendarCheck, primary: true },
  { to: "/", end: true, label: "Today", icon: MessageSquare },
  { to: "/history", end: false, label: "History", icon: CalendarDays },
  { to: "/goals", end: false, label: "Goals", icon: Target },
  { to: "/patterns", end: false, label: "Patterns", icon: Sparkles },
  { to: "/settings", end: false, label: "Settings", icon: SettingsIcon },
];

interface Props {
  layoutId: string;
  onNavigate?: () => void;
}

export function NavMenu({ layoutId, onNavigate }: Props) {
  const me = useMe();
  const reduce = useReducedMotion();
  const isReflectionDay =
    me.data != null &&
    typeof me.data.local_weekday === "number" &&
    me.data.local_weekday === me.data.reflection_weekday;
  // On the reflection day the button stays all day (even after
  // completing — Done view / replay remain reachable). On carry-over
  // days it shows only while the week's reflection is pending.
  const showWeekly =
    isReflectionDay || me.data?.reflection_pending === true;
  const navItems = showWeekly
    ? baseNavItems
    : baseNavItems.filter((item) => item.to !== "/weekly");

  return (
    <nav className="flex-1 space-y-0.5">
      {navItems.map((item) => (
        <NavLink
          key={item.to}
          to={item.to}
          end={item.end}
          onClick={onNavigate}
          className={({ isActive }) =>
            cn(
              "relative flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors",
              item.primary
                ? isActive
                  ? "text-primary-foreground"
                  : "text-primary hover:text-primary-foreground"
                : isActive
                  ? "text-foreground"
                  : "text-muted-foreground hover:text-foreground hover:bg-secondary/60",
            )
          }
        >
          {({ isActive }) => (
            <>
              {isActive ? (
                <motion.span
                  layoutId={layoutId}
                  className={cn(
                    "absolute inset-0 rounded-md",
                    item.primary ? "bg-primary" : "bg-secondary",
                  )}
                  transition={
                    reduce
                      ? { duration: 0 }
                      : { type: "spring", stiffness: 380, damping: 32 }
                  }
                />
              ) : item.primary ? (
                <span className="absolute inset-0 rounded-md bg-primary/10 ring-1 ring-inset ring-primary/30" />
              ) : null}
              <item.icon className="relative h-4 w-4" />
              <span className="relative">{item.label}</span>
            </>
          )}
        </NavLink>
      ))}
    </nav>
  );
}
