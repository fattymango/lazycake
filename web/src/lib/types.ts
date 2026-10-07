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
  /** The customer asked for it to be stopped and it hasn't finished yet ("Stopping"). */
  cancel_requested?: boolean;
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
  /** Lifetime traffic through this gateway. */
  traffic: TrafficTotals;
}

/** Named from the customer's side: what tasks sent to their service, and what came back. */
export interface TrafficTotals {
  received_from_tasks_bytes: number;
  sent_to_tasks_bytes: number;
  connections: number;
}

export interface GatewayTraffic {
  range: "7d" | "30d";
  step: "hour" | "day";
  totals: TrafficTotals;
  series: { at_ms: number; received_from_tasks_bytes: number; sent_to_tasks_bytes: number }[];
  busiest_tasks: ({ task_id: string } & TrafficTotals)[];
}

export interface TaskTraffic {
  totals: TrafficTotals;
  rows: ({ gateway_id: string; service: string } & TrafficTotals)[];
  /** Per 5-minute bucket, as the customer's own gateway counted it. */
  series: { at_ms: number; received_from_tasks_bytes: number; sent_to_tasks_bytes: number }[];
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

/** Result of GET-less POST /api/portal/customer/gateways/{id}/test. */
export type GatewayTestStatus = "green" | "yellow" | "red" | "grey";

export interface GatewayServiceTest {
  name: string;
  port: number;
  ok: boolean;
  error?: string;
  ms?: number;
  /** False when the gateway never replied about this service (an older gateway). */
  answered: boolean;
}

export interface GatewayTestResult {
  status: GatewayTestStatus;
  connected: boolean;
  rtt_ms?: number;
  verified: boolean;
  services: GatewayServiceTest[];
  tested_at_ms: number;
}

/** Machine usage as reported by its agent (display only; the host controls the agent). */
export interface TaskShare {
  cpu_cores: number;
  memory_bytes: number;
  /** Bytes moved through the tunnel during the period (sums, not averages). */
  tunnel_out_bytes: number;
  tunnel_in_bytes: number;
}

export interface UsagePoint {
  at_ms: number;
  samples: number;
  host_cpu_busy: number;
  host_mem_used_bytes: number;
  disk_used_bytes: number;
  tasks_cpu_cores: number;
  tasks_memory_bytes: number;
  tunnel_out_bytes: number;
  tunnel_in_bytes: number;
  per_task: Record<string, TaskShare>;
}

export interface UsageLatest {
  at_ms: number;
  host_cpu_busy: number;
  host_cpu_count: number;
  host_mem_total_bytes: number;
  host_mem_used_bytes: number;
  disk_total_bytes: number;
  disk_used_bytes: number;
  tasks_cpu_cores: number;
  tasks_memory_bytes: number;
  tasks: { task_id: string; cpu_cores: number; memory_bytes: number }[];
}

export interface NodeUsage {
  /** False until the machine's agent has reported usage at least once. */
  supported: boolean;
  range: "24h" | "7d";
  step: "5m" | "hour";
  latest?: UsageLatest;
  /** Tasks that have their own band, biggest first; the rest are under OTHERS_KEY. */
  tasks: string[];
  series: UsagePoint[];
}

export interface TaskUsageTotals {
  core_seconds: number;
  peak_memory_bytes: number;
  tunnel_bytes_to_gateway: number;
  tunnel_bytes_to_task: number;
}

/** A task as listed on a machine page, with what it used (when its agent reported it). */
export type NodeTask = Task & { usage?: TaskUsageTotals };

/** One reading of a task's own use, about every 15 s (as reported by the machine's agent). */
export interface TaskReading {
  at_ms: number;
  cpu_cores: number;
  memory_bytes: number;
  /** Bytes moved through the tunnel since the previous reading. */
  tunnel_out_bytes: number;
  tunnel_in_bytes: number;
}

export interface TaskUsage {
  supported: boolean;
  /** Whether the machine the task ran on has ever reported usage. False means its agent is too old to. */
  agent_reports_usage: boolean;
  limits: { cores: number; memory_mb: number };
  points: TaskReading[];
  summary?: TaskUsageTotals;
}
