// The one place tone -> classes is decided, so Badge, StatCard, icons tiles,
// progress bars and alerts all agree on what "success" or "danger" looks like.
export type Tone = "neutral" | "accent" | "success" | "warning" | "danger" | "info";

export const toneClasses: Record<Tone, { text: string; bg: string; border: string; solid: string; dot: string; ring: string }> = {
  neutral: { text: "text-muted", bg: "bg-raised", border: "border-border", solid: "bg-muted", dot: "bg-muted", ring: "ring-border" },
  accent: {
    text: "text-accent",
    bg: "bg-accent/10",
    border: "border-accent/25",
    solid: "bg-accent",
    dot: "bg-accent",
    ring: "ring-accent/30",
  },
  success: {
    text: "text-success",
    bg: "bg-success/10",
    border: "border-success/25",
    solid: "bg-success",
    dot: "bg-success",
    ring: "ring-success/30",
  },
  warning: {
    text: "text-warning",
    bg: "bg-warning/10",
    border: "border-warning/25",
    solid: "bg-warning",
    dot: "bg-warning",
    ring: "ring-warning/30",
  },
  danger: {
    text: "text-danger",
    bg: "bg-danger/10",
    border: "border-danger/25",
    solid: "bg-danger",
    dot: "bg-danger",
    ring: "ring-danger/30",
  },
  info: { text: "text-info", bg: "bg-info/10", border: "border-info/25", solid: "bg-info", dot: "bg-info", ring: "ring-info/30" },
};
