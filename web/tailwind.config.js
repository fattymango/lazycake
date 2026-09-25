/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./customer.html", "./provider.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        // Same palette as internal/coordinator/dashboard/index.html, so the
        // /ops dashboard and the two portals read as one product.
        bg: "#0b0f14",
        panel: "#121826",
        border: "#232c3d",
        text: "#e6edf3",
        muted: "#8b98a9",
        accent: "#4fd1c5",
        good: "#3fb950",
        warn: "#d29922",
        bad: "#f85149",
        queued: "#8b98a9",
        running: "#4fd1c5",
        dispatched: "#58a6ff",
      },
      fontFamily: {
        sans: ["-apple-system", "BlinkMacSystemFont", "Segoe UI", "sans-serif"],
        mono: ["ui-monospace", "SFMono-Regular", "Menlo", "monospace"],
      },
    },
  },
  plugins: [],
};
