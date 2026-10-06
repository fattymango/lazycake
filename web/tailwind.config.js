import defaultTheme from "tailwindcss/defaultTheme";

// Every color is a CSS variable from src/ui/tokens.css, written as an RGB
// triplet so opacity modifiers work (bg-accent/10). Nothing in this file or in
// any component should contain a raw hex value.
const color = (name) => `rgb(var(--${name}) / <alpha-value>)`;

/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  darkMode: ["selector", '[data-theme="dark"]'],
  theme: {
    extend: {
      colors: {
        bg: color("bg"),
        surface: color("surface"),
        raised: color("raised"),
        overlay: color("overlay"),
        border: color("border"),
        "border-strong": color("border-strong"),
        fg: color("fg"),
        muted: color("muted"),
        subtle: color("subtle"),
        accent: { DEFAULT: color("accent"), fg: color("accent-fg") },
        success: color("success"),
        warning: color("warning"),
        danger: color("danger"),
        info: color("info"),
        term: {
          bg: color("term-bg"),
          fg: color("term-fg"),
          muted: color("term-muted"),
          err: color("term-err"),
          border: color("term-border"),
        },
      },
      fontFamily: {
        sans: ['"Inter Variable"', ...defaultTheme.fontFamily.sans],
        mono: ['"JetBrains Mono Variable"', ...defaultTheme.fontFamily.mono],
      },
      fontSize: {
        "2xs": ["0.6875rem", { lineHeight: "1rem", letterSpacing: "0.01em" }],
      },
      borderRadius: {
        lg: "0.5rem",
        xl: "0.75rem",
        "2xl": "1rem",
      },
      boxShadow: {
        card: "0 1px 2px rgb(var(--shadow) / calc(var(--shadow-strength) * 0.5)), 0 0 0 1px rgb(var(--border) / 0.0)",
        pop: "0 12px 32px -8px rgb(var(--shadow) / var(--shadow-strength)), 0 2px 6px rgb(var(--shadow) / calc(var(--shadow-strength) * 0.4))",
        glow: "0 0 0 4px rgb(var(--accent) / 0.18)",
      },
      keyframes: {
        "fade-in": { from: { opacity: "0" }, to: { opacity: "1" } },
        "slide-up": { from: { opacity: "0", transform: "translateY(6px)" }, to: { opacity: "1", transform: "translateY(0)" } },
        "pop-in": { from: { opacity: "0", transform: "scale(0.97)" }, to: { opacity: "1", transform: "scale(1)" } },
        shimmer: { "100%": { transform: "translateX(100%)" } },
        "ping-soft": { "75%, 100%": { transform: "scale(2.2)", opacity: "0" } },
      },
      animation: {
        "fade-in": "fade-in 150ms ease-out",
        "slide-up": "slide-up 200ms ease-out",
        "pop-in": "pop-in 150ms ease-out",
        shimmer: "shimmer 1.6s infinite",
        "ping-soft": "ping-soft 1.8s cubic-bezier(0, 0, 0.2, 1) infinite",
      },
    },
  },
  plugins: [],
};
