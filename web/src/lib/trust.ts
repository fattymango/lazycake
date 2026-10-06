import type { Tone } from "@/ui/tone";

/** Colour a 0..1 trust score: reliable, middling, or poor. */
export function trustTone(score: number): Tone {
  return score >= 0.8 ? "success" : score >= 0.5 ? "warning" : "danger";
}
