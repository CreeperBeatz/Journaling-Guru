import { useEffect, useState } from "react";
import { useTheme } from "next-themes";
import { Monitor, Moon, Sun } from "lucide-react";

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";

const MODES = [
  { value: "light", label: "Light", icon: Sun },
  { value: "dark", label: "Dark", icon: Moon },
  { value: "system", label: "System", icon: Monitor },
] as const;

export function AppearanceCard() {
  const { theme, setTheme } = useTheme();

  // next-themes resolves on the client only — placeholder until then to avoid
  // a hydration mismatch on the active-mode pill.
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);
  const currentMode = (mounted ? theme : "system") ?? "system";

  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-serif">Appearance</CardTitle>
        <CardDescription>
          Choose light or dark mode. Follows your system by default.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-5">
        <div className="space-y-2">
          <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Mode
          </p>
          <div
            role="radiogroup"
            aria-label="Theme mode"
            className="inline-flex rounded-md border border-border bg-muted p-1"
          >
            {MODES.map(({ value, label, icon: Icon }) => {
              const active = currentMode === value;
              return (
                <button
                  key={value}
                  type="button"
                  role="radio"
                  aria-checked={active}
                  onClick={() => setTheme(value)}
                  className={cn(
                    "inline-flex h-8 items-center gap-1.5 rounded px-3 text-sm font-medium transition-colors",
                    "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 ring-offset-background",
                    active
                      ? "bg-background text-foreground shadow-xs"
                      : "text-muted-foreground hover:text-foreground",
                  )}
                >
                  <Icon className="h-3.5 w-3.5" />
                  {label}
                </button>
              );
            })}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
