import { NavLink } from "react-router-dom";
import { LogOut } from "lucide-react";

import { ThemeToggle } from "@/components/ui/theme-toggle";
import { cn } from "@/lib/utils";
import { useEntries } from "@/features/journal/hooks";
import { formatShortHumanDate } from "@/lib/date";

import type { User } from "@/features/auth/api";
import { NavMenu } from "./NavMenu";

interface Props {
  user: User | null;
  onSignOut: () => void;
  signingOut: boolean;
}

export function Sidebar({ user, onSignOut, signingOut }: Props) {
  const entries = useEntries();
  const today = entries.data?.local_date;
  const dateLabel = today ? formatShortHumanDate(today) : null;

  return (
    <aside className="hidden h-screen w-60 flex-col border-r border-border bg-card/40 p-4 md:flex md:sticky md:top-0">
      <NavLink to="/" className="px-2 pt-3 font-serif italic text-xl tracking-tight leading-none">
        Journaling Guru
      </NavLink>
      {dateLabel ? (
        <p className="px-2 pb-3 pt-1 font-mono text-xs text-muted-foreground">
          {dateLabel}
        </p>
      ) : (
        <div className="pb-3" />
      )}
      <div className="mt-2 flex flex-1 flex-col">
        <NavMenu layoutId="sidebar-nav-active" />
      </div>
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
