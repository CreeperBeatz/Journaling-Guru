import { NavLink } from "react-router-dom";
import { History, PenLine, Settings as SettingsIcon, Sparkles } from "lucide-react";
import { motion } from "motion/react";

import { cn } from "@/lib/utils";

// Bottom-tab is space-constrained on mobile — Questions moves into the
// Settings page in Phase 4. Five core surfaces: Today, History,
// Summaries, Settings (Questions tab inside).
const tabs = [
  { to: "/", end: true, label: "Today", icon: PenLine },
  { to: "/history", end: false, label: "History", icon: History },
  { to: "/summaries", end: false, label: "Reflect", icon: Sparkles },
  { to: "/settings", end: false, label: "Settings", icon: SettingsIcon },
];

export function BottomTabBar() {
  return (
    <nav
      aria-label="Primary navigation"
      className={cn(
        "fixed inset-x-0 bottom-0 z-40 border-t border-border bg-background/85 backdrop-blur-md md:hidden",
        "pb-[env(safe-area-inset-bottom)]",
      )}
    >
      <ul className="grid grid-cols-4">
        {tabs.map((tab) => (
          <li key={tab.to}>
            <NavLink
              to={tab.to}
              end={tab.end}
              className={({ isActive }) =>
                cn(
                  "relative flex h-14 flex-col items-center justify-center gap-0.5 text-[11px] font-medium transition-colors",
                  isActive ? "text-foreground" : "text-muted-foreground",
                )
              }
            >
              {({ isActive }) => (
                <>
                  {isActive ? (
                    <motion.span
                      layoutId="bottom-nav-pill"
                      className="absolute inset-x-3 inset-y-1.5 rounded-md bg-secondary"
                      transition={{ type: "spring", stiffness: 380, damping: 32 }}
                    />
                  ) : null}
                  <tab.icon className="relative h-5 w-5" aria-hidden="true" />
                  <span className="relative">{tab.label}</span>
                </>
              )}
            </NavLink>
          </li>
        ))}
      </ul>
    </nav>
  );
}
