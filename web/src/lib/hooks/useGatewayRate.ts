import { apiGet } from "@/lib/api";
import type { GatewayRate } from "@/lib/types";
import { useAsync } from "./useAsync";
import { useInterval } from "./useInterval";

/** How often the live bandwidth is re-read. The data only changes every few seconds, so faster would show nothing new. */
export const RATE_POLL_MS = 5_000;

/** The live bandwidth of all of the account's gateways, or of one. Polls while the page is visible. */
export function useGatewayRate(gatewayId?: string) {
  const path = `/api/portal/customer/gateways/rate${gatewayId ? `?gateway=${encodeURIComponent(gatewayId)}` : ""}`;
  const rate = useAsync((s) => apiGet<GatewayRate>(path, s), [path]);
  useInterval(() => rate.reload(), RATE_POLL_MS);
  return rate;
}
