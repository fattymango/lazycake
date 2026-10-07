// What a provider chooses to lend on the "Add a machine" page, and the checks that go with it.
// The agent is the real authority: it refuses to start with an offer bigger than the machine has.
// These checks give the same answer earlier, once the provider has told us what the machine has.

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

/** What the provider types: friendly units. */
export interface OfferInput {
  cores: number;
  memoryGB: number;
  storageGB: number;
  networkMbps: number;
}

export type OfferField = keyof OfferInput;

// The same bounds the coordinator enforces (portalapi/provider_handlers.go), so a typo is caught before it is sent.
export const OFFER_BOUNDS = {
  cores: { min: 0.25, max: 1024 },
  memoryGB: { min: 0.25, max: 16384 },
  storageGB: { min: 1, max: 1_048_576 },
  networkMbps: { min: 1, max: 100_000 },
} as const;

export const OFFER_DEFAULTS: OfferInput = { cores: 2, memoryGB: 2, storageGB: 8, networkMbps: 100 };

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

/** The most the machine can give, in the units the provider types. */
export function maxOffer(caps: MachineCaps | null): OfferInput {
  return {
    cores: caps ? Math.min(caps.cores, OFFER_BOUNDS.cores.max) : OFFER_BOUNDS.cores.max,
    memoryGB: caps ? caps.memoryMB / 1024 : OFFER_BOUNDS.memoryGB.max,
    storageGB: caps ? caps.diskMB / 1024 : OFFER_BOUNDS.storageGB.max,
    networkMbps: caps?.networkMbps ?? OFFER_BOUNDS.networkMbps.max,
  };
}

/** The defaults, never above what the machine has. */
export function defaultOffer(caps: MachineCaps | null): OfferInput {
  const max = maxOffer(caps);
  const round = (n: number) => Math.floor(n * 100) / 100;
  return {
    cores: Math.min(OFFER_DEFAULTS.cores, round(max.cores)),
    memoryGB: Math.min(OFFER_DEFAULTS.memoryGB, round(max.memoryGB)),
    storageGB: Math.min(OFFER_DEFAULTS.storageGB, round(max.storageGB)),
    networkMbps: Math.min(OFFER_DEFAULTS.networkMbps, Math.floor(max.networkMbps)),
  };
}

const LABEL: Record<OfferField, string> = { cores: "CPU cores", memoryGB: "memory", storageGB: "storage", networkMbps: "network" };
const UNIT: Record<OfferField, string> = { cores: "cores", memoryGB: "GB", storageGB: "GB", networkMbps: "Mbps" };

/** A sentence for every value that is out of range or bigger than the machine, keyed by field. */
export function offerProblems(o: OfferInput, caps: MachineCaps | null): Partial<Record<OfferField, string>> {
  const max = maxOffer(caps);
  const out: Partial<Record<OfferField, string>> = {};
  for (const f of Object.keys(OFFER_BOUNDS) as OfferField[]) {
    const v = o[f];
    const { min } = OFFER_BOUNDS[f];
    if (!Number.isFinite(v) || v <= 0) out[f] = `Enter how much ${LABEL[f]} to lend.`;
    else if (v < min) out[f] = `At least ${min} ${UNIT[f]}.`;
    else if (caps && f !== "networkMbps" && v > max[f] + 1e-9)
      out[f] = `This machine only has ${fmt(max[f])} ${UNIT[f]}${f === "storageGB" ? " free" : ""}.`;
    else if (caps && f === "networkMbps" && caps.networkMbps !== null && v > caps.networkMbps)
      out[f] = `This machine's network link is ${caps.networkMbps} Mbps.`;
    else if (v > OFFER_BOUNDS[f].max) out[f] = `At most ${fmt(OFFER_BOUNDS[f].max)} ${UNIT[f]}.`;
  }
  return out;
}

/** Rounded down, never up: a limit shown as "15" must not be one a person can type and be refused for. */
function fmt(n: number): string {
  return String(Math.floor(n * 100) / 100);
}

/** The request body the coordinator expects. */
export function offerRequest(o: OfferInput) {
  return {
    cores: o.cores,
    memory_mb: Math.round(o.memoryGB * 1024),
    disk_mb: Math.round(o.storageGB * 1024),
    network_mbps: Math.round(o.networkMbps),
  };
}
