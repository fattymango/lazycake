/**
 * Whole seconds left until `until` (a timestamp in ms), never negative. Used by
 * the frontend-only cooldown on buttons that trigger a server-side check.
 */
export function secondsLeft(until: number, now: number): number {
  return Math.max(0, Math.ceil((until - now) / 1000));
}
