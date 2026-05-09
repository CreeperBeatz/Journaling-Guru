import { Outlet, Link } from "react-router-dom";

import { ThemeToggle } from "@/components/ui/theme-toggle";
import { Toaster } from "@/components/ui/sonner";

// Minimal layout for un-authed surfaces (/auth/login, /auth/verify, /health).
// No sidebar, no bottom-tab — those would imply an account exists.
export function AuthLayout() {
  return (
    <div className="flex min-h-svh flex-col bg-background text-foreground">
      <header className="flex items-center justify-between px-6 pt-[max(env(safe-area-inset-top),1rem)] pb-2">
        <Link to="/" className="font-serif italic text-xl tracking-tight">
          Journaling Guru
        </Link>
        <ThemeToggle />
      </header>
      <main className="flex flex-1 items-center justify-center px-6 py-8">
        <div className="w-full max-w-md">
          <Outlet />
        </div>
      </main>
      <Toaster />
    </div>
  );
}
