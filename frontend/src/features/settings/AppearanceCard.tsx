import { useEffect, useState } from "react";
import { useTheme } from "next-themes";
import { Check, Monitor, Moon, Sun } from "lucide-react";

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";
import {
  PALETTE_DESCRIPTION,
  PALETTE_LABEL,
  PALETTE_SWATCH,
  PALETTES,
  type Palette,
  usePalette,
} from "@/lib/palette";

const MODES = [
  { value: "light", label: "Light", icon: Sun },
  { value: "dark", label: "Dark", icon: Moon },
  { value: "system", label: "System", icon: Monitor },
] as const;

export function AppearanceCard() {
  const { theme, setTheme } = useTheme();
  const { palette, setPalette } = usePalette();

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
          Choose a color scheme. Light/dark follows your system by default; the
          palette tints the entire app — page, ink, and accents.
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

        <div className="space-y-2">
          <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Palette
          </p>
          <div
            role="radiogroup"
            aria-label="Color palette"
            className="grid grid-cols-2 gap-2 sm:grid-cols-5"
          >
            {PALETTES.map((p) => (
              <PaletteOption
                key={p}
                palette={p}
                active={p === palette}
                onSelect={() => setPalette(p)}
              />
            ))}
          </div>
          <p className="pt-1 text-xs text-muted-foreground">
            {PALETTE_DESCRIPTION[palette]}
          </p>
        </div>
      </CardContent>
    </Card>
  );
}

function PaletteOption({
  palette,
  active,
  onSelect,
}: {
  palette: Palette;
  active: boolean;
  onSelect: () => void;
}) {
  const [bg, primary, accent] = PALETTE_SWATCH[palette];
  return (
    <button
      type="button"
      role="radio"
      aria-checked={active}
      aria-label={`${PALETTE_LABEL[palette]} palette`}
      onClick={onSelect}
      className={cn(
        "group relative flex flex-col gap-2 rounded-lg border p-2.5 text-left transition-colors",
        "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 ring-offset-background",
        active
          ? "border-primary ring-1 ring-primary"
          : "border-border hover:border-foreground/30",
      )}
    >
      <span
        className="relative flex h-12 items-end gap-1 overflow-hidden rounded-md border border-border/80 p-1.5"
        style={{ backgroundColor: bg }}
        aria-hidden
      >
        <span
          className="h-5 w-5 rounded-full ring-1 ring-black/5"
          style={{ backgroundColor: primary }}
        />
        <span
          className="h-3 w-3 rounded-full ring-1 ring-black/5"
          style={{ backgroundColor: accent }}
        />
      </span>
      <span className="flex items-center justify-between text-xs font-medium">
        {PALETTE_LABEL[palette]}
        {active ? <Check className="h-3.5 w-3.5 text-primary" aria-hidden /> : null}
      </span>
    </button>
  );
}
