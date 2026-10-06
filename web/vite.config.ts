import { resolve } from "path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// One entry point: which portal (customer/provider) renders is decided at
// runtime by the signed-in account's own role (App.tsx), not by which HTML
// page was loaded - see src/shared/auth.tsx's module doc comment for why
// that changed from the original two-entry-point design. Proxies
// /api/portal to a local coordinator during `npm run dev` so the app can
// be developed against a real backend without CORS wrangling. `npm run
// build` emits dist/, which internal/coordinator/webassets go:embeds as-is.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": resolve(__dirname, "src"),
    },
  },
  build: {
    outDir: "dist",
  },
  server: {
    proxy: {
      "/api/portal": {
        target: process.env.LAZYCAKE_PROXY || "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },
});
