// What a provider chooses to lend on the "Add a machine" page, and the checks that go with it.
//
// Everything is a whole number. The slider offers checkpoints and stops at a sensible top; the number
// field can go beyond the slider (up to a hard ceiling that no value can overflow). The agent is the real
// authority: it refuses to start with an offer bigger than the machine has. The checks here give the same
// answer earlier, once the provider has told us what the machine has.

/** The public image the machine runs. A bare name resolves against docker.io/library and is denied. */
export const AGENT_IMAGE = "docker.io/fattymango/lazycake-agent:latest";

/** Prints one line with what the machine has; --network=host so it can read the real link speed. */
export const CAPACITY_COMMAND = `podman run --rm --network=host ${AGENT_IMAGE} capacity`;

export interface MachineCaps {
  cores: number;
  memoryMB: number;
  /** Free disk where the agent keeps its data. */
  diskMB: number;
  /** The fastest physical link, or null when the machine doesn't report one (virtual machines, wifi). */
  networkMbps: number | null;
}

/** What the provider types: whole numbers in friendly units. */
export interface OfferInput {
  cores: number;
  memoryGB: number;
  storageGB: number;
  networkMbps: number;
}

export type OfferField = keyof OfferInput;
export const OFFER_FIELDS: OfferField[] = ["cores", "memoryGB", "storageGB", "networkMbps"];

/** The checkpoints the slider snaps to. The last one is where the slider tops out. */
export const SLIDER_STOPS: Record<OfferField, number[]> = {
  cores: [1, 2, 4, 8, 16, 24],
  memoryGB: [1, 2, 4, 8, 16, 32, 64],
  storageGB: [5, 10, 20, 50, 100],
  networkMbps: [10, 25, 50, 100, 250, 500, 1000],
};

/**
 * The absolute limits of what can be typed. These mirror the coordinator (portalapi/provider_handlers.go)
 * and are chosen so that no value, once converted to megabytes, can overflow a 32-bit field.
 */
export const HARD_LIMITS: Record<OfferField, { min: number; max: number }> = {
  cores: { min: 1, max: 1024 },
  memoryGB: { min: 1, max: 16 * 1024 },
  storageGB: { min: 1, max: 1024 * 1024 },
  networkMbps: { min: 1, max: 100_000 },
};

export const OFFER_DEFAULTS: OfferInput = { cores: 2, memoryGB: 2, storageGB: 10, networkMbps: 100 };

export const FIELD_LABEL: Record<OfferField, string> = { cores: "CPU", memoryGB: "Memory", storageGB: "Storage", networkMbps: "Network" };
export const FIELD_UNIT: Record<OfferField, string> = { cores: "cores", memoryGB: "GB", storageGB: "GB", networkMbps: "Mbps" };

/**
 * Turn what a person typed into a whole number, safely: only digits are kept (no signs, decimals, exponents
 * or spaces), leading zeros go, and the result is clamped to the field's hard ceiling so a huge paste can
 * never become a huge number. Empty input is NaN (the field is empty, not zero).
 */
export function parseWhole(text: string, field: OfferField): number {
  const digits = text.replace(/\D/g, "").replace(/^0+(?=\d)/, "");
  if (digits === "") return Number.NaN;
  // Anything with more digits than the ceiling has is over the ceiling, whatever it says: no Number() of a giant string.
  const ceiling = HARD_LIMITS[field].max;
  if (digits.length > String(ceiling).length) return ceiling;
  return Math.min(Number(digits), ceiling);
}

/** Reads the line `agent capacity` prints: "cores=12 memory_mb=15314 disk_mb=441802 network_mbps=1000". */
export function parseCapacity(text: string): MachineCaps | null {
  const get = (key: string) => {
    const m = new RegExp(`(?:^|\\s)${key}=([0-9]+(?:\\.[0-9]+)?)(?=\\s|$)`).exec(text);
    return m ? Number(m[1]) : null;
  };
  const cores = get("cores");
  const memoryMB = get("memory_mb");
  const diskMB = get("disk_mb");
  const network = get("network_mbps");
  if (cores === null || memoryMB === null || diskMB === null || cores <= 0 || memoryMB <= 0 || diskMB <= 0) return null;
  return { cores, memoryMB, diskMB, networkMbps: network !== null && network > 0 ? network : null };
}

/** The most the machine can give, as whole numbers in the units the provider types (rounded down). */
export function maxOffer(caps: MachineCaps | null): OfferInput {
  return {
    cores: caps ? Math.min(Math.floor(caps.cores), HARD_LIMITS.cores.max) : HARD_LIMITS.cores.max,
    memoryGB: caps ? Math.floor(caps.memoryMB / 1024) : HARD_LIMITS.memoryGB.max,
    storageGB: caps ? Math.floor(caps.diskMB / 1024) : HARD_LIMITS.storageGB.max,
    networkMbps: caps?.networkMbps != null ? Math.floor(caps.networkMbps) : HARD_LIMITS.networkMbps.max,
  };
}

/** The defaults, never above what the machine has (and never below one). */
export function defaultOffer(caps: MachineCaps | null): OfferInput {
  const max = maxOffer(caps);
  const pick = (f: OfferField) => Math.max(HARD_LIMITS[f].min, Math.min(OFFER_DEFAULTS[f], max[f]));
  return { cores: pick("cores"), memoryGB: pick("memoryGB"), storageGB: pick("storageGB"), networkMbps: pick("networkMbps") };
}

/** A sentence for every value that is out of range or bigger than the machine, keyed by field. */
export function offerProblems(o: OfferInput, caps: MachineCaps | null): Partial<Record<OfferField, string>> {
  const max = maxOffer(caps);
  const out: Partial<Record<OfferField, string>> = {};
  for (const f of OFFER_FIELDS) {
    const v = o[f];
    const { min, max: ceiling } = HARD_LIMITS[f];
    const unit = FIELD_UNIT[f];
    if (!Number.isInteger(v) || v <= 0) out[f] = `Enter a whole number of ${unit === "GB" ? "GB" : unit}.`;
    else if (v < min) out[f] = `At least ${min} ${unit}.`;
    else if (v > ceiling) out[f] = `At most ${ceiling.toLocaleString("en-US")} ${unit}.`;
    else if (caps && f !== "networkMbps" && v > max[f])
      out[f] = `This machine only has ${max[f].toLocaleString("en-US")} ${unit}${f === "storageGB" ? " free" : ""}.`;
    else if (caps && f === "networkMbps" && caps.networkMbps !== null && v > max.networkMbps)
      out[f] = `This machine's network link is ${max.networkMbps.toLocaleString("en-US")} Mbps.`;
  }
  return out;
}

/** Past this much, tell the provider what it costs them. Relative to the machine when known, else absolute. */
const BIG_ABSOLUTE: OfferInput = { cores: 8, memoryGB: 16, storageGB: 50, networkMbps: 500 };
const BIG_SHARE = 0.5;

/** Which of the offered amounts are big enough that the provider's own use of the machine will feel it. */
export function bigOffers(o: OfferInput, caps: MachineCaps | null): OfferField[] {
  const max = maxOffer(caps);
  return OFFER_FIELDS.filter((f) => {
    const v = o[f];
    if (!Number.isInteger(v) || v <= 0) return false;
    const known = caps && (f !== "networkMbps" || caps.networkMbps !== null);
    return known ? v >= max[f] * BIG_SHARE : v >= BIG_ABSOLUTE[f];
  });
}

/** The request body the coordinator expects. */
export function offerRequest(o: OfferInput) {
  return {
    cores: o.cores,
    memory_mb: o.memoryGB * 1024,
    disk_mb: o.storageGB * 1024,
    network_mbps: o.networkMbps,
  };
}

/**
 * Where the slider's thumb sits for a value: 0 .. stops.length-1. Between two checkpoints it sits between them
 * (a typed 3 is halfway to 4), and past the last one it stays at the end.
 */
export function sliderPosition(stops: number[], value: number): number {
  if (!Number.isFinite(value) || value <= stops[0]) return 0;
  const last = stops.length - 1;
  if (value >= stops[last]) return last;
  let i = 0;
  while (stops[i + 1] <= value) i++;
  return i + (value - stops[i]) / (stops[i + 1] - stops[i]);
}

/** The checkpoint a thumb position snaps to. */
export function snapToStop(stops: number[], position: number): number {
  const i = Math.max(0, Math.min(stops.length - 1, Math.round(position)));
  return stops[i];
}

/**
 * The checkpoints to show for a field: the normal ones, but never above what the machine has, and ending
 * exactly at what it has when that is below the slider's usual top (so a 12-core machine gets 1, 2, 4, 8, 12).
 */
export function stopsFor(field: OfferField, machineMax: number | null): number[] {
  const stops = SLIDER_STOPS[field];
  if (machineMax === null || machineMax >= stops[stops.length - 1]) return stops;
  const below = stops.filter((s) => s < machineMax);
  const out = [...below, machineMax];
  return out.length >= 2 ? out : [HARD_LIMITS[field].min, Math.max(machineMax, HARD_LIMITS[field].min + 1)];
}
