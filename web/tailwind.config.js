/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        // Same semantic palette as internal/coordinator/dashboard/index.html
        // (PLAN.md §6 names this file as the source of truth to reuse from),
        // extended with a couple of surface tokens the single ops page
        // never needed: a raised/hover surface distinct from the base panel,
        // and a slightly dimmer border for nested dividers.
        bg: "#0a0e13",
        panel: "#121826",
        panel2: "#1a2233",
        border: "#232c3d",
        borderSoft: "#1a2230",
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
      boxShadow: {
        panel: "0 1px 2px rgba(0,0,0,0.4), 0 0 0 1px rgba(255,255,255,0.02)",
        popover: "0 12px 32px rgba(0,0,0,0.5), 0 0 0 1px rgba(255,255,255,0.04)",
      },
      borderRadius: {
        xl2: "0.875rem",
      },
    },
  },
  plugins: [],
};
