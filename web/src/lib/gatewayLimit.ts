/**
 * How a gateway's bandwidth cap reads in the dashboard. `mbps` is what the gateway reported: a number of Mbps (0 = it enforces no
 * limit) or null/undefined when it never said, which is a gateway built before it reported limits and must not read as "no limit".
 */
export function bandwidthLimit(mbps: number | null | undefined): { text: string; reported: boolean } {
  if (mbps === null || mbps === undefined) return { text: "Not reported", reported: false };
  if (mbps === 0) return { text: "No limit", reported: true };
  return { text: `${mbps} Mbps`, reported: true };
}
