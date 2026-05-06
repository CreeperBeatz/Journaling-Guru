import { Outlet, Link } from "react-router-dom";

export function App() {
  return (
    <div className="min-h-full flex flex-col">
      <header className="border-b border-border">
        <div className="mx-auto max-w-3xl px-6 py-4 flex items-center justify-between">
          <Link to="/" className="font-semibold tracking-tight">
            JournAI
          </Link>
          <span className="text-xs text-muted-foreground">v2 · phase 2</span>
        </div>
      </header>
      <main className="flex-1">
        <div className="mx-auto max-w-3xl px-6 py-8">
          <Outlet />
        </div>
      </main>
    </div>
  );
}
