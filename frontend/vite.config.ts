import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";
import { VitePWA } from "vite-plugin-pwa";
import path from "node:path";

// We use `injectManifest` (not `generateSW`) so the push handler in
// src/sw/push-handler.ts is the actual service worker. Workbox precaching
// still runs because vite-plugin-pwa injects the manifest into our SW.
export default defineConfig(({ mode, command }) => {
  const env = loadEnv(mode, path.resolve(__dirname, ".."), "VITE_");
  const apiBase = env.VITE_API_BASE || "http://localhost:8080";
  const isDev = command === "serve";
  // Set by start.sh --lan / --tunnel: bind 0.0.0.0 so phones on the LAN can
  // reach Vite. (For tunnels, cloudflared connects via 127.0.0.1, so this is
  // only needed for --lan.)
  const devHost =
    process.env.VITE_DEV_HOST === "true" || process.env.VITE_DEV_HOST === "1";

  return {
    plugins: [
      react(),
      VitePWA({
        strategies: "injectManifest",
        srcDir: "src/sw",
        filename: "push-handler.ts",
        registerType: "autoUpdate",
        injectRegister: "auto",
        manifest: false, // shipped statically from public/manifest.webmanifest
        injectManifest: {
          globPatterns: ["**/*.{js,css,html,svg,png,ico,webmanifest,woff2}"],
          // Bumped because motion + Radix push the SW precache past the
          // 2 MiB default. We're still well under any realistic budget.
          maximumFileSizeToCacheInBytes: 5 * 1024 * 1024,
        },
        devOptions: {
          enabled: true,
          type: "module",
        },
      }),
    ],
    build: {
      rollupOptions: {
        output: {
          manualChunks: {
            "react-vendor": ["react", "react-dom", "react-router-dom"],
            "query-vendor": ["@tanstack/react-query"],
            "motion-vendor": ["motion"],
            "radix-vendor": [
              "@radix-ui/react-dialog",
              "@radix-ui/react-alert-dialog",
              "@radix-ui/react-select",
              "@radix-ui/react-tabs",
              "@radix-ui/react-separator",
              "@radix-ui/react-slot",
            ],
            icons: ["lucide-react"],
            markdown: ["react-markdown", "remark-gfm"],
          },
        },
      },
    },
    resolve: {
      alias: {
        "@": path.resolve(__dirname, "src"),
      },
    },
    server: {
      host: devHost || undefined,
      // Dev only: allow any Host header so LAN IPs and cloudflared/ngrok
      // tunnel domains aren't blocked. The dev server is local — no risk.
      allowedHosts: isDev ? true : undefined,
      port: 5173,
      // /auth/* lives on the SPA (login + verify pages). Backend mounts
      // its auth endpoints under /api/auth/*, so only /api needs proxying.
      proxy: {
        "/api": {
          target: apiBase,
          changeOrigin: true,
        },
      },
    },
    envDir: path.resolve(__dirname, ".."),
  };
});
