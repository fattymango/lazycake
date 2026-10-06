// Wire shapes for /api/portal/*, mirrored from internal/coordinator/store
// types the same way internal/coordinator/dashboard's nodeView/taskView are:
// flat, snake_case JSON, independent of the Go struct field names so either
// side can evolve without the other silently breaking. Phase 7A
// (docs/01-dashboard-portals/IMPLEMENTATION.md tasks 7.1-7.5) has not
// landed yet as of this frontend build - these types are the frontend's
// half of that contract, written from the plan's Appendix and from
// internal/coordinator/store/types.go, and are the first place to check
// when wiring up the real backend.

export type Role = "customer" | "provider";

export type TaskState = "queued" | "reserved" | "dispatched" | "running" | "succeeded" | "failed" | "fenced" | "abandoned" | "cancelled";

// Whoami is GET /api/portal/me's shape: resolves whichever role's session
// cookie is present, so the app can render the right portal without
// already knowing which one before it asks (that's the whole point - see
// auth.tsx).
export interface Whoami {
  account_id: string;
  username: string;
  role: Role;
}

export interface Me {
  account_id: string;
  username: string;
  role: Role;
  balance_micros: number;
  available_balance_micros: number;
}

export interface ProviderMe {
  account_id: string;
  username: string;
  role: "provider";
  lifetime_earnings_micros: number;
}

export interface TunnelTarget {
  gateway_id: string;
  hostname: string;
  port: number;
}

export interface Task {
  id: string;
  state: TaskState;
  image: string;
  entrypoint?: string[];
  args?: string[];
  env?: Record<string, string>;
  node_id?: string;
  cores: number;
  memory_mb: number;
  disk_mb?: number;
  wall_timeout_s?: number;
  tunnel_targets?: TunnelTarget[];
  /** How many times the task has been restarted; absent on a first run. */
  attempt?: number;
  exit_code?: number;
  exit_reason?: string;
  created_at_ms: number;
  started_at_ms?: number;
  finished_at_ms?: number;
}

export interface SubmitTaskRequest {
  image: string;
  entrypoint?: string[];
  args?: string[];
  env?: Record<string, string>;
  workdir?: string;
  cores: number;
  memory_mb: number;
  disk_mb?: number;
  wall_timeout_s?: number;
  tunnel_targets?: TunnelTarget[];
  idempotency_key?: string;
}

export interface LogLine {
  seq: number;
  stream: "stdout" | "stderr";
  at_ms: number;
  line: string;
}

export interface GatewayService {
  name: string;
  port: number;
}

export interface Gateway {
  id: string;
  label: string;
  connected: boolean;
  services: GatewayService[];
  created_at_ms: number;
}

export interface CreateGatewayRequest {
  label: string;
  services: GatewayService[];
}

export interface CreateGatewayResponse extends Gateway {
  install_token: string;
}

export interface LedgerEntry {
  id: string;
  task_id: string;
  kind: "charge" | "credit";
  amount_micros: number;
  created_at_ms: number;
}

export interface Node {
  id: string;
  hostname: string;
  arch: string;
  connected: boolean;
  offer_cores: number;
  offer_memory_mb: number;
  offer_disk_mb: number;
  trust_score: number;
  last_heartbeat_at_ms?: number;
  created_at_ms: number;
}

export interface InstallToken {
  token: string;
  install_command: string;
}

// Events delivered over the account-scoped SSE stream
// (GET /api/portal/{customer,provider}/events, PLAN.md §3/§5.4 - the same
// events.Bus task 6.1 built, filtered to the caller's own account).
export type PortalEvent =
  | { type: "task_state"; task_id: string; state: TaskState; node_id?: string }
  | { type: "node_connected"; node_id: string }
  | { type: "node_disconnected"; node_id: string }
  | { type: "balance"; balance_micros: number };
