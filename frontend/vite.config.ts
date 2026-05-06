import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";
import { VitePWA } from "vite-plugin-pwa";
import path from "node:path";

// We use `injectManifest` (not `generateSW`) so the push handler in
// src/sw/push-handler.ts is the actual service worker. Workbox precaching
// still runs because vite-plugin-pwa injects the manifest into our SW.
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, path.resolve(__dirname, ".."), "VITE_");
  const apiBase = env.VITE_API_BASE || "http://localhost:8080";

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
          globPatterns: ["**/*.{js,css,html,svg,png,ico,webmanifest}"],
        },
        devOptions: {
          enabled: true,
          type: "module",
        },
      }),
    ],
    resolve: {
      alias: {
        "@": path.resolve(__dirname, "src"),
      },
    },
    server: {
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
