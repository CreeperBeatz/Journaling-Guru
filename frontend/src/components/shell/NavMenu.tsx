import { NavLink } from "react-router-dom";
import {
  CalendarDays,
  MessageSquare,
  Settings as SettingsIcon,
  Sparkles,
  Target,
} from "lucide-react";
import { motion } from "motion/react";

import { cn } from "@/lib/utils";

const navItems = [
  { to: "/", end: true, label: "Today", icon: MessageSquare },
  { to: "/history", end: false, label: "History", icon: CalendarDays },
  { to: "/goals", end: false, label: "Goals", icon: Target },
  { to: "/summary", end: false, label: "Summary", icon: Sparkles },
  { to: "/settings", end: false, label: "Settings", icon: SettingsIcon },
];

interface Props {
  layoutId: string;
  onNavigate?: () => void;
}

export function NavMenu({ layoutId, onNavigate }: Props) {
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
              isActive
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
                  className="absolute inset-0 rounded-md bg-secondary"
                  transition={{ type: "spring", stiffness: 380, damping: 32 }}
                />
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
