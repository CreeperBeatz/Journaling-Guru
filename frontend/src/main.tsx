import React from "react";
import ReactDOM from "react-dom/client";
import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "react-router-dom";

import "@fontsource-variable/geist";
import "@fontsource/instrument-serif/400.css";
import "@fontsource-variable/jetbrains-mono";

import { router } from "./router";
import { queryClient } from "./lib/queryClient";
import { ThemeProvider } from "./components/ui/theme-provider";
import { PaletteSync } from "./components/ui/palette-sync";
import { initInstallCapture } from "./features/install/install";
import "./styles/index.css";

// Capture beforeinstallprompt at module load — it fires once, very early,
// and is gone if no listener is attached when it dispatches.
initInstallCapture();

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ThemeProvider>
      <PaletteSync />
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </ThemeProvider>
  </React.StrictMode>
);
