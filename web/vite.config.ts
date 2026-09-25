import { resolve } from "path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Two entry points sharing src/shared/*; both proxy /api/portal to a local
// coordinator during `npm run dev` so the portals can be developed against
// a real backend without CORS wrangling. `npm run build` emits both pages
// into dist/, which internal/coordinator/webassets go:embeds as-is.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@shared": resolve(__dirname, "src/shared"),
    },
  },
  build: {
    outDir: "dist",
    rollupOptions: {
      input: {
        main: resolve(__dirname, "index.html"),
        customer: resolve(__dirname, "customer.html"),
        provider: resolve(__dirname, "provider.html"),
      },
    },
  },
  server: {
    proxy: {
      "/api/portal": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },
});
