import { NavLink } from "react-router-dom";
import {
  CalendarDays,
  LogOut,
  MessageSquare,
  Settings as SettingsIcon,
  Sparkles,
  Target,
} from "lucide-react";
import { motion } from "motion/react";

import { ThemeToggle } from "@/components/ui/theme-toggle";
import { cn } from "@/lib/utils";

import type { User } from "@/features/auth/api";

const navItems = [
  { to: "/", end: true, label: "Today", icon: MessageSquare },
  { to: "/history", end: false, label: "History", icon: CalendarDays },
  { to: "/goals", end: false, label: "Goals", icon: Target },
  { to: "/summary", end: false, label: "Summary", icon: Sparkles },
  { to: "/settings", end: false, label: "Settings", icon: SettingsIcon },
];

interface Props {
  user: User | null;
  onSignOut: () => void;
  signingOut: boolean;
}

export function Sidebar({ user, onSignOut, signingOut }: Props) {
  return (
    <aside className="hidden h-screen w-60 flex-col border-r border-border bg-card/40 p-4 md:flex md:sticky md:top-0">
      <NavLink to="/" className="px-2 py-3 font-serif italic text-xl tracking-tight">
        JournAI
      </NavLink>
      <nav className="mt-4 flex-1 space-y-0.5">
        {navItems.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.end}
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
                    layoutId="sidebar-nav-active"
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
      <div className="mt-auto space-y-2 border-t border-border/60 pt-3">
        {user ? (
          <p className="truncate px-2 text-xs text-muted-foreground" title={user.email}>
            {user.email}
          </p>
        ) : null}
        <div className="flex items-center justify-between gap-2 px-1">
          <ThemeToggle />
          <button
            type="button"
            onClick={onSignOut}
            disabled={signingOut}
            aria-label="Sign out"
            title="Sign out"
            className={cn(
              "inline-flex h-9 items-center gap-2 rounded-md px-2.5 text-xs",
              "text-muted-foreground hover:bg-secondary hover:text-foreground",
              "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 ring-offset-background",
              "disabled:opacity-50",
            )}
          >
            <LogOut className="h-3.5 w-3.5" />
            {signingOut ? "Signing out…" : "Sign out"}
          </button>
        </div>
      </div>
    </aside>
  );
}
