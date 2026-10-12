#!/usr/bin/env python3
"""Prints SQL that fills a LazyCake database with 30 days of busy, realistic DEMO history, for screenshots and demos (a LinkedIn post, a talk).

Nothing in it can run: every task it creates is already finished (succeeded, failed or cancelled), so the scheduler has nothing to dispatch and no agent does
any work. Every row it adds has an id starting with DEMO_TAG (tsk_01M5DEMO..., nod_01M5DEMO..., gw_01M5DEMO..., led_01M5DEMO...), so --clean removes exactly
that. Wipe the database when you are done: this is invented data, not measurements.

    python3 web/e2e/seed_showcase.py --customer kassab --provider kassab1 [--real-gateway gw_x:files:8000 ...] > /tmp/showcase.sql
    python3 web/e2e/seed_showcase.py --customer kassab --provider kassab1 --clean > /tmp/clean.sql

What it does, in order:
  1. Moves the account's earlier tasks (real test runs, with their ledger and meter rows) 45 days into the past, so they fall outside every chart and the 7-day
     success rate is computed over the demo history (--clean moves them back).
  2. Adds machines for the provider, gateways for the customer, ~30 days of tasks (a weekly rhythm, growing volume, ~99% success), their meters, ledger
     charges and credits, per-task usage, machine usage (5-minute buckets for 10 days, 30-second samples for the last 2 hours, with a few offline gaps) and
     gateway traffic.
  3. Moves the two accounts' balances by the demo charges and credits.
Safe to run once per database (run --clean first to run it again).
"""
import argparse
import datetime as dt
import heapq
import json
import math
import random

ap = argparse.ArgumentParser()
ap.add_argument("--customer", default="alice")
ap.add_argument("--provider", default="bob")
ap.add_argument("--days", type=int, default=30)
ap.add_argument("--seed", type=int, default=2026)
ap.add_argument("--real-gateway", action="append", default=[], help="existing gateway to give traffic: id:service:port (repeatable)")
ap.add_argument("--shift-old-days", type=int, default=45)
ap.add_argument("--balance", type=float, default=2500.0, help="dollars the customer 'had' before the demo charges")
ap.add_argument("--clean", action="store_true")
ap.add_argument("--hero", action="store_true", help="put a failed task, 3 running tasks and 2 queued tasks at the top of the customer's task list (run after the main seed, just before the photos); nothing is dispatched or run")
ap.add_argument("--live", action="store_true", help="refresh 'right now': the last 5 minutes of gateway bandwidth and the machines' heartbeats (run it just before taking screenshots)")
ap.add_argument("--live-gateway", action="append", default=[], help="gateway id:Mbps (repeatable) to give live bandwidth, e.g. gw_x:180")
args = ap.parse_args()

TAG = "01M5DEMO"
CUST = f"(SELECT account_id FROM portal_credentials WHERE username='{args.customer}')"
PROV = f"(SELECT account_id FROM portal_credentials WHERE username='{args.provider}')"
out = []
w = out.append


def q(s):
    return "'" + str(s).replace("'", "''") + "'"


def ts(t):
    return "'" + t.strftime("%Y-%m-%d %H:%M:%S+00") + "'"


def jsonb(v):
    return q(json.dumps(v)) + "::jsonb"


def arr(xs):
    return "ARRAY[" + ",".join(q(x) for x in xs) + "]::text[]" if xs else "NULL"


# ------------------------------------------------------------------------------------------------------------------ live
def emit_live_telemetry():
    """Keeps the running demo tasks and the machines carrying them looking alive: heartbeats, fresh usage readings up to now, the machine's 'now' panel."""
    w(f"UPDATE nodes SET last_heartbeat_at = now(), connected = true WHERE id LIKE '%{TAG}%';")
    w(f"UPDATE gateways SET connected = true WHERE id LIKE '%{TAG}%';")
    run = f"t.state = 'running' AND t.id LIKE '%{TAG}%'"
    # one reading per running task every 15 s since it started (the task page's Resource use chart)
    w(f"INSERT INTO task_usage_samples (task_id, at, cpu_cores, memory_bytes, tunnel_out, tunnel_in) "
      f"SELECT t.id, g.at, t.cpu_cores * 0.93 * (0.92 + 0.16 * random()), (t.memory_mb::bigint * 1048576 * 0.72 * (1 - exp(-extract(epoch FROM g.at - t.started_at) / 70.0)))::bigint, 0, 0 "
      f"FROM tasks t CROSS JOIN LATERAL generate_series(t.started_at + interval '15 seconds', now(), interval '15 seconds') g(at) WHERE {run} ON CONFLICT DO NOTHING;")
    # machine readings every 30 s for the last 12 minutes, with the running tasks in them
    w("INSERT INTO node_usage_samples (node_id, at, host_cpu_busy, cpu_count, mem_used, mem_total, disk_used, disk_total, tasks_cpu, tasks_mem, tasks) "
      "SELECT n.id, g.at, LEAST(0.97, 0.02 + COALESCE(SUM(t.cpu_cores * 0.93), 0) / n.offer_cores + random() * 0.012), n.offer_cores::int, "
      "(2.4 * 2^30 + COALESCE(SUM(t.memory_mb::bigint * 1048576 * 0.6), 0))::bigint, (n.offer_memory_mb::bigint * 1048576 * 1.25)::bigint, (n.offer_disk_mb::bigint * 1048576 * 0.36)::bigint, (n.offer_disk_mb::bigint * 1048576)::bigint, "
      "COALESCE(SUM(t.cpu_cores * 0.93), 0), COALESCE(SUM(t.memory_mb::bigint * 1048576 * 0.6), 0)::bigint, "
      "COALESCE(jsonb_agg(jsonb_build_object('id', t.id, 'cpu', round((t.cpu_cores * 0.93)::numeric, 3), 'mem', (t.memory_mb::bigint * 1048576 * 0.6)::bigint, 'out', 0, 'in', 0)) FILTER (WHERE t.id IS NOT NULL), '[]'::jsonb) "
      f"FROM nodes n CROSS JOIN LATERAL generate_series(date_trunc('minute', now()) - interval '12 minutes', now(), interval '30 seconds') g(at) "
      f"LEFT JOIN tasks t ON t.node_id = n.id AND {run} AND t.started_at <= g.at WHERE n.id LIKE '%{TAG}%' GROUP BY n.id, g.at, n.offer_cores, n.offer_memory_mb, n.offer_disk_mb ON CONFLICT DO NOTHING;")
    w("UPDATE node_usage_latest l SET at = now(), sample = jsonb_set(jsonb_set(jsonb_set(l.sample, '{At}', to_jsonb(to_char(now() at time zone 'utc', 'YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"'))), "
      "'{Tasks}', COALESCE((SELECT jsonb_agg(jsonb_build_object('TaskID', t.id, 'CPUCores', round((t.cpu_cores * 0.93)::numeric, 3), 'MemoryBytes', (t.memory_mb::bigint * 1048576 * 0.6)::bigint, 'TunnelToGateway', 0, 'TunnelToTask', 0)) "
      f"FROM tasks t WHERE t.node_id = l.node_id AND {run}), 'null'::jsonb)), "
      f"'{{HostCPUBusy}}', to_jsonb(round(LEAST(0.97, 0.03 + COALESCE((SELECT SUM(t.cpu_cores * 0.93) FROM tasks t WHERE t.node_id = l.node_id AND {run}), 0) / (SELECT offer_cores FROM nodes WHERE id = l.node_id))::numeric, 4))) "
      f"WHERE l.node_id LIKE '%{TAG}%';")


if args.hero:
    rh = random.Random(args.seed)
    CROCK32 = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
    nowh = dt.datetime.now(dt.timezone.utc).replace(microsecond=0)

    def hid(code):
        return "tsk_" + TAG + "H" + code + "".join(rh.choice(CROCK32) for _ in range(15))

    def img(prefix):
        return f"(SELECT image FROM tasks WHERE image LIKE '{prefix}@%' AND id LIKE '%{TAG}%' AND id NOT LIKE '%{TAG}H%' LIMIT 1)"

    def nodeid(host):
        return f"(SELECT id FROM nodes WHERE hostname = '{host}' AND id LIKE '%{TAG}%')"

    GWPG = f"(SELECT id FROM gateways WHERE label = 'production-postgres' AND id LIKE '%{TAG}%')"
    day = nowh.strftime("%Y-%m-%d")
    like = f"'%{TAG}H%'"
    w("BEGIN;")
    for t in ("task_logs", "task_usage_samples", "task_meters", "task_usage_totals", "gateway_traffic", "task_gateway_totals"):
        w(f"DELETE FROM {t} WHERE task_id LIKE {like};")
    w(f"DELETE FROM ledger_entries WHERE task_id LIKE {like};")
    w(f"DELETE FROM tasks WHERE id LIKE {like};")
    heroes = []  # (code, state, image prefix, entrypoint, args, cores, mem, wall, created_ago_s, started_after_s, node host, gateway?, log builder)

    def ago(s):
        return ts(nowh - dt.timedelta(seconds=s))

    def tasks_row(tid, state, image, ent, av, cores, mem, wall, created_s, started_s, finished_s, node, gw, arch, code, reason):
        gwj = f"jsonb_build_array(jsonb_build_object('gateway_id', {GWPG}, 'hostname', 'postgres', 'port', 5432))" if gw else "'[]'::jsonb"
        gwa = f"ARRAY[{GWPG}]::text[]" if gw else "ARRAY[]::text[]"
        st = ago(started_s) if started_s is not None else "NULL"
        fi = ago(finished_s) if finished_s is not None else "NULL"
        w(f"INSERT INTO tasks (id, account_id, state, image, entrypoint, args, env, workdir, cpu_cores, memory_mb, disk_mb, wall_timeout_s, req_arch, req_isolation, gateway_ids, delivery, max_attempts, attempt, node_id, exit_code, exit_reason, started_at, finished_at, created_at, tunnel_targets) "
          f"VALUES ({q(tid)}, {CUST}, {q(state)}::task_state, {image}, {arr(ent)}, {arr(av)}, '{{}}'::jsonb, '', {cores}, {mem}, 2048, {wall}, {q(arch)}, 'podman', {gwa}, 'at_most_once', 1, 1, {node}, "
          f"{code}, {q(reason) if reason else 'NULL'}, {st}, {fi}, {ago(created_s)}, {gwj});")

    def log(tid, rows):
        vals = [f"({q(tid)}, {i}, {q(stream)}, {ago(s)}, {q(text)})" for i, (stream, s, text) in enumerate(rows, 1)]
        w("INSERT INTO task_logs (task_id, seq, stream, at, line) VALUES " + ",".join(vals) + ";")

    def iso(s):
        return (nowh - dt.timedelta(seconds=s)).strftime("%Y-%m-%dT%H:%M:%SZ")

    # --- running (oldest first so the newest is created last)
    r1, r2, r3 = hid("R1"), hid("R2"), hid("R3")
    tasks_row(r1, "running", img("docker.io/pytorch/pytorch"), None, ["python", "eval.py", "--model", "resnet50", "--shard", "7"], 4, 4096, 3600, 372, 360, None, nodeid("fra1-atlas-rack-01"), False, "amd64", "NULL", None)
    log(r1, [("stdout", 360 - k * 12, t) for k, t in enumerate(
        ["loading model resnet50 (25.6M parameters) ... done in 4.1s", "dataset shard 7/16: 31,250 images, batch size 64 (489 batches)"] +
        [f"batch {20 * i}/489  top1={0.74 + i * 0.0021:.3f}  top5={0.915 + i * 0.001:.3f}  {i * 54 + 31} img/s" for i in range(1, 25)])])
    tasks_row(r2, "running", img("docker.io/library/python"), None, ["python", "-m", "etl.run", "--day", day, "--source", "postgres.prod", "--batch-size", "5000"], 2, 2048, 1800, 188, 182, None, nodeid("fra1-atlas-rack-02"), True, "amd64", "NULL", None)
    log(r2, [("stdout", 182 - k * 15, t) for k, t in enumerate(
        [f"{iso(182)} INFO  etl.run starting day={day} source=postgres.prod batch_size=5000", f"{iso(181)} INFO  connected to postgres:5432 through the gateway, server version 16.4", f"{iso(180)} INFO  extracting public.orders (est. 1,204,330 rows)"] +
        [f"{iso(180 - 15 * i)} INFO  batch {i}/241 ok (5000 rows, {13.2 + i % 4 * 0.9:.1f}s)" for i in range(1, 11)])])
    tasks_row(r3, "running", img("docker.io/library/rust"), ["sh", "-c"], ["cargo test --release -- --shard 3/8"], 2, 1024, 600, 92, 84, None, nodeid("hel1-ax41-build-03"), False, "amd64", "NULL", None)
    log(r3, [("stdout", 84 - k * 6, t) for k, t in enumerate(
        ["Compiling lazycake-core v0.9.4", "Finished `test` profile [optimized] target(s) in 41.2s", "Running unittests src/lib.rs", "running 412 tests"] +
        [f"test {m} ... ok" for m in ["auth::login_ok", "auth::session_expiry", "billing::price_round_trip", "billing::hold_release", "sched::fills_node", "sched::respects_memory", "relay::reconnects", "tunnel::noise_handshake", "usage::bucket_sum", "api::submit_idempotent"]])])
    # --- failed
    f1 = hid("F1")
    tasks_row(f1, "failed", img("docker.io/library/python"), None, ["python", "-m", "etl.load", "--source", "postgres.prod", "--batch-size", "5000"], 1, 1024, 300, 640, 636, 589, nodeid("fra1-atlas-rack-02"), True, "amd64", "1", "exited")
    log(f1, [("stdout", 636, f"{iso(636)} INFO  etl.load starting source=postgres.prod batch_size=5000"), ("stdout", 635, f"{iso(635)} INFO  connected to postgres:5432 through the gateway, server version 16.4"),
             ("stdout", 634, f"{iso(634)} INFO  extracting table public.orders (est. 2,418,330 rows)"), ("stdout", 620, f"{iso(620)} INFO  batch 1/484 ok (5000 rows, 15.8s)"),
             ("stdout", 605, f"{iso(605)} INFO  batch 2/484 ok (5000 rows, 15.1s)"), ("stderr", 590, f"{iso(590)} WARN  slow query on batch 3: 31.4s (threshold 20s)"),
             ("stderr", 589, "Traceback (most recent call last):"), ("stderr", 589, '  File "/app/etl/load.py", line 142, in <module>'), ("stderr", 589, "    main()"),
             ("stderr", 589, '  File "/app/etl/load.py", line 118, in main'), ("stderr", 589, "    cursor.execute(QUERY, params)"),
             ("stderr", 589, "psycopg.errors.QueryCanceled: canceling statement due to statement timeout"), ("stdout", 589, f"{iso(589)} ERROR load failed after 2 of 484 batches; exiting with status 1")])
    price = 100 + int((50 * 1 + 10 + 2 * 2) * 47 * 1.41)
    w(f"INSERT INTO task_meters (task_id, node_id, started_at, finished_at, duration_s, normalised_s) VALUES ({q(f1)}, {nodeid('fra1-atlas-rack-02')}, {ago(636)}, {ago(589)}, 47, {47 * 1.41:.2f});")
    w(f"INSERT INTO ledger_entries (id, task_id, account_id, kind, amount_micros, created_at) VALUES ({q('led_' + f1[4:])}, {q(f1)}, {CUST}, 'charge', {-price}, {ago(589)}), ({q('ldp_' + f1[4:])}, {q(f1)}, {PROV}, 'credit', {price}, {ago(589)});")
    w(f"UPDATE accounts SET balance_micros = balance_micros - {price} WHERE id = {CUST};")
    w(f"UPDATE accounts SET balance_micros = balance_micros + {price} WHERE id = {PROV};")
    w(f"INSERT INTO task_usage_totals (task_id, node_id, core_seconds, peak_memory_bytes, tunnel_to_gateway, tunnel_to_task, samples, updated_at) VALUES ({q(f1)}, {nodeid('fra1-atlas-rack-02')}, 41.2, 612000000, 41000, 11800000, 3, {ago(589)});")
    # --- queued: an architecture no machine offers, so the scheduler can never claim one of these (nothing is ever dispatched or run)
    q1, q2 = hid("Q1"), hid("Q2")
    tasks_row(q2, "queued", img("docker.io/pytorch/pytorch"), None, ["python", "eval.py", "--model", "vit-b16", "--shard", "11"], 4, 4096, 3600, 41, None, None, "NULL", False, "riscv64", "NULL", None)
    tasks_row(q1, "queued", img("docker.io/jrottenberg/ffmpeg"), None, ["-i", "/in/clip-218.mp4", "-c:v", "libx264", "-preset", "medium", "/out/clip-218.mp4"], 2, 2048, 1800, 18, None, None, "NULL", False, "riscv64", "NULL", None)
    emit_live_telemetry()
    w("COMMIT;")
    print("\n".join(out))
    raise SystemExit

if args.live:
    rl = random.Random()
    now = dt.datetime.now(dt.timezone.utc).replace(microsecond=0)
    w("BEGIN;")
    emit_live_telemetry()
    for spec in args.live_gateway:
        gid, mbps = spec.split(":")
        mbps = float(mbps)
        task = f"(SELECT id FROM tasks WHERE id LIKE '%{TAG}%' AND gateway_ids @> ARRAY[{q(gid)}]::text[] ORDER BY created_at DESC LIMIT 1)"
        w(f"DELETE FROM task_gateway_samples WHERE gateway_id = {q(gid)} AND task_id LIKE '%{TAG}%' AND at > now() - interval '15 minutes';")
        rows = []
        for k in range(32):
            at = now - dt.timedelta(seconds=10 * k)
            wave = 0.55 + 0.35 * math.sin(k / 3.1) + rl.uniform(-0.12, 0.12)
            down = mbps * max(wave, 0.08) * 1e6 / 8 * 10  # gateway -> tasks, bytes per 10 s
            up = down * rl.uniform(0.06, 0.2)
            rows.append(f"({task}, {q(gid)}, {ts(at)}, {int(up)}, {int(down)})")
        w("INSERT INTO task_gateway_samples (task_id, gateway_id, at, bytes_to_local, bytes_to_task) VALUES\n" + ",\n".join(rows) + "\nON CONFLICT DO NOTHING;")
    w("COMMIT;")
    print("\n".join(out))
    raise SystemExit

# ----------------------------------------------------------------------------------------------------------------- clean
if args.clean:
    like = f"'%{TAG}%'"
    w("BEGIN;")
    # gateway_totals are lifetime sums kept incrementally: take the demo traffic back out before deleting it.
    w(f"UPDATE gateway_totals g SET bytes_to_local = GREATEST(g.bytes_to_local - d.l, 0), bytes_to_task = GREATEST(g.bytes_to_task - d.t, 0), connections = GREATEST(g.connections - d.c, 0) "
      f"FROM (SELECT gateway_id, SUM(bytes_to_local) l, SUM(bytes_to_task) t, SUM(connections) c FROM gateway_traffic WHERE task_id LIKE {like} GROUP BY gateway_id) d WHERE g.gateway_id = d.gateway_id;")
    w(f"UPDATE accounts SET balance_micros = balance_micros + COALESCE((SELECT -SUM(amount_micros) FROM ledger_entries WHERE kind='charge' AND task_id LIKE {like} AND account_id = {CUST}), 0) WHERE id = {CUST};")
    w(f"UPDATE accounts SET balance_micros = balance_micros - COALESCE((SELECT SUM(amount_micros) FROM ledger_entries WHERE kind='credit' AND task_id LIKE {like} AND account_id = {PROV}), 0) WHERE id = {PROV};")
    w(f"DELETE FROM ledger_entries WHERE id LIKE {like} OR task_id LIKE {like};")
    for t in ("task_logs", "task_usage_samples", "node_task_usage", "gateway_traffic", "task_gateway_samples", "task_gateway_totals", "task_meters", "task_usage_totals"):
        w(f"DELETE FROM {t} WHERE task_id LIKE {like};")
    w(f"DELETE FROM tasks WHERE id LIKE {like};")
    w(f"DELETE FROM nodes WHERE id LIKE {like};")
    w(f"DELETE FROM gateways WHERE id LIKE {like};")
    d = args.shift_old_days
    w(f"UPDATE tasks SET created_at = created_at + interval '{d} days', started_at = started_at + interval '{d} days', finished_at = finished_at + interval '{d} days' "
      f"WHERE account_id = {CUST} AND created_at < now() - interval '{d - 5} days' AND created_at > now() - interval '{d + 40} days';")
    w(f"UPDATE ledger_entries SET created_at = created_at + interval '{d} days' WHERE created_at < now() - interval '{d - 5} days' AND created_at > now() - interval '{d + 40} days' AND (account_id = {CUST} OR account_id = {PROV});")
    w(f"UPDATE task_meters SET started_at = started_at + interval '{d} days', finished_at = finished_at + interval '{d} days' WHERE started_at < now() - interval '{d - 5} days' AND started_at > now() - interval '{d + 40} days';")
    w("COMMIT;")
    print("\n".join(out))
    raise SystemExit

# ------------------------------------------------------------------------------------------------------------- generate
rnd = random.Random(args.seed)
NOW = dt.datetime.now(dt.timezone.utc).replace(microsecond=0)
END = NOW - dt.timedelta(minutes=2)  # nothing finishes after this: the last minutes stay free for real activity
START = NOW - dt.timedelta(days=args.days)
CROCK = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
counter = {"n": 0}


def mid(prefix):
    counter["n"] += 1
    n, tail = counter["n"], ""
    for _ in range(6):
        tail = CROCK[n % 32] + tail
        n //= 32
    return f"{prefix}_{TAG}{tail}" + "".join(rnd.choice(CROCK) for _ in range(26 - len(TAG) - 6))


# --- machines (the provider's): (host, arch, cores, mem_mb, disk_mb, mbps, bench, trust, offline windows [(hour_from, hour_to) daily UTC], one-off gaps)
def hrs(h):
    return dt.timedelta(hours=h)


MACHINES = [
    ("fra1-atlas-rack-01", "amd64", 8, 32768, 900000, 1000, 1.46, 0.99, [], []),
    ("fra1-atlas-rack-02", "amd64", 8, 32768, 900000, 1000, 1.41, 0.98, [], [(NOW - dt.timedelta(days=2, hours=13), NOW - dt.timedelta(days=2, hours=10, minutes=30))]),
    ("hel1-ax41-build-03", "amd64", 4, 16384, 450000, 1000, 1.22, 0.97, [], [(NOW - dt.timedelta(days=5, hours=20), NOW - dt.timedelta(days=5, hours=18))]),
    ("studio-mac-mini-m2", "arm64", 4, 8192, 240000, 500, 1.08, 0.96, [(23.5, 6.5)], []),
]
nodes = []
for host, arch, cores, mem, disk, mbps, bench, trust, daily_off, gaps in MACHINES:
    nodes.append(dict(id=mid("nod"), host=host, arch=arch, cores=cores, mem=mem, disk=disk, mbps=mbps, bench=bench, trust=trust, daily_off=daily_off, gaps=gaps,
                      active=[], created=NOW - dt.timedelta(days=rnd.randint(26, 41))))


def online(n, t):
    for a, b in n["gaps"]:
        if a <= t < b:
            return False
    h = t.hour + t.minute / 60
    for a, b in n["daily_off"]:
        if (a <= h or h < b) if a > b else (a <= h < b):
            return False
    return t >= n["created"]


# --- gateways: real ones given on the command line plus two demo ones
gateways = []  # dict(id, label, service, port, demo)
for spec in args.real_gateway:
    gid, svc, port = spec.split(":")
    gateways.append(dict(id=gid, service=svc, port=int(port), demo=False))
GW_PG = dict(id=mid("gw"), label="production-postgres", service="postgres", port=5432, demo=True, mbps=500)
GW_MET = dict(id=mid("gw"), label="internal-metrics", service="metrics", port=9100, demo=True, mbps=100)
gateways += [GW_PG, GW_MET]
FILES = next((g for g in gateways if g["service"] == "files"), None)
if FILES is None:  # no real file gateway given (a local demo database): add a demo one
    FILES = dict(id=mid("gw"), label="internal-files", service="files", port=8000, demo=True, mbps=1000)
    gateways.append(FILES)

# --- the customer's workloads
IMG = {
    "py": "docker.io/library/python@sha256:" + "".join(rnd.choice("0123456789abcdef") for _ in range(64)),
    "node": "docker.io/library/node@sha256:" + "".join(rnd.choice("0123456789abcdef") for _ in range(64)),
    "ffmpeg": "docker.io/jrottenberg/ffmpeg@sha256:" + "".join(rnd.choice("0123456789abcdef") for _ in range(64)),
    "torch": "docker.io/pytorch/pytorch@sha256:" + "".join(rnd.choice("0123456789abcdef") for _ in range(64)),
    "alpine": "docker.io/library/alpine@sha256:c64c687cbea9300178b30c95835354e34c4e4febc4badfe27102879de0483b5e",
    "pg": "docker.io/library/postgres@sha256:" + "".join(rnd.choice("0123456789abcdef") for _ in range(64)),
    "rust": "docker.io/library/rust@sha256:" + "".join(rnd.choice("0123456789abcdef") for _ in range(64)),
    "lcbench": "docker.io/fattymango/lcbench@sha256:65e8c5765cc43af8cf9e6a71165d31954dbfee495aa1ada997fb35b8c672e1db",
}
TENANTS = ["acme", "globex", "initech", "umbrella", "hooli", "stark", "wayne", "wonka"]


def lognorm(lo, hi):
    mu, sigma = math.log(math.sqrt(lo * hi)), math.log(hi / lo) / 3.2
    return min(max(rnd.lognormvariate(mu, sigma), lo), hi)


# name, weight, image key, cores choices, mem choices, duration range (s), cpu utilisation range, gateway key, gateway prob, (MB from the gateway to the task), (MB from the task to the gateway), argv builder, arch
TEMPLATES = [
    ("ci", 22, "rust", [2, 2, 4], [1024, 2048], (20, 140), (0.75, 0.97), "files", 0.15, (2, 40), (1, 60), lambda d, n: ["sh", "-c", f"cargo test --release -- --shard {n % 8 + 1}/8"], "amd64"),
    ("report", 18, "node", [0.5, 0.5, 1], [512, 1024], (4, 32), (0.35, 0.7), "metrics", 0.4, (0.1, 5), (0.5, 12), lambda d, n: ["node", "render.js", "--tenant", TENANTS[n % 8], "--format", "pdf"], "amd64"),
    ("probe", 9, "alpine", [0.25], [128], (1, 6), (0.05, 0.25), "metrics", 0.9, (0.01, 0.05), (0.005, 0.05), lambda d, n: ["sh", "-c", "curl -fsS http://metrics:9100/metrics | head -c 2000 >/dev/null"], "amd64"),
    ("etl", 14, "py", [2, 2, 4], [2048, 4096], (120, 840), (0.55, 0.9), "postgres", 1.0, (60, 700), (5, 120), lambda d, n: ["python", "-m", "etl.run", "--day", d, "--source", "postgres.prod", "--batch-size", "5000"], "amd64"),
    ("backup", 3, "pg", [1, 2], [1024], (240, 1200), (0.3, 0.6), "postgres", 1.0, (300, 2400), (1, 20), lambda d, n: ["sh", "-c", f"pg_dump -h postgres -Fc app_{TENANTS[n % 8]} > /out/{d}.dump"], "amd64"),
    ("transcode", 12, "ffmpeg", [2, 2, 4], [2048, 4096], (180, 1500), (0.85, 0.98), "files", 0.35, (80, 900), (60, 700), lambda d, n: ["-i", f"/in/clip-{n % 500:03d}.mp4", "-c:v", "libx264", "-preset", "medium", f"/out/clip-{n % 500:03d}.mp4"], "amd64"),
    ("eval", 9, "torch", [4, 4], [4096, 8192], (900, 4500), (0.8, 0.97), "files", 0.2, (200, 2000), (5, 60), lambda d, n: ["python", "eval.py", "--model", rnd.choice(["resnet50", "vit-b16", "convnext-t"]), "--shard", str(n % 16)], "amd64"),
    ("train", 4, "torch", [8], [8192], (1800, 5400), (0.88, 0.99), "files", 0.3, (400, 3000), (5, 40), lambda d, n: ["python", "train.py", "--epochs", "3", "--seed", str(n % 97)], "amd64"),
    ("montecarlo", 5, "py", [1, 2, 4], [512, 1024, 2048], (120, 1800), (0.9, 0.99), None, 0, (0, 0), (0, 0), lambda d, n: ["python", "-m", "pricing.mc", "--paths", str(rnd.choice([200000, 500000, 1000000])), "--seed", str(n)], "amd64"),
    ("armbuild", 4, "rust", [2, 4], [2048], (200, 900), (0.8, 0.95), "files", 0.2, (5, 60), (2, 40), lambda d, n: ["sh", "-c", "cargo build --release --target aarch64-unknown-linux-gnu"], "arm64"),
    ("bench", 3, "lcbench", [0.5, 1, 2], [512], (30, 60), (0.9, 1.0), None, 0, (0, 0), (0, 0), lambda d, n: [], "amd64"),
]
TW = [t[1] for t in TEMPLATES]
avg_core_s = sum(t[1] * (sum(t[3]) / len(t[3])) * math.sqrt(t[5][0] * t[5][1]) * (sum(t[6]) / 2) for t in TEMPLATES) / sum(TW)
fleet_cores = sum(n["cores"] for n in nodes)
TARGET_UTIL = 0.2  # average over the month; the weekday daytime peaks reach roughly twice that
HOUR_SHAPE = [0.55, 0.5, 0.5, 0.45, 0.5, 0.6, 0.8, 1.05, 1.3, 1.5, 1.55, 1.5, 1.35, 1.4, 1.55, 1.5, 1.4, 1.2, 1.0, 0.9, 0.8, 0.7, 0.65, 0.6]
base_per_day = TARGET_UTIL * fleet_cores * 86400 / avg_core_s

tasks = []
created_times = []
for d in range(args.days + 1):  # +1: the partial day that contains NOW
    day = (START + dt.timedelta(days=d)).replace(hour=0, minute=0, second=0)
    growth = 0.55 + 0.9 * min(d / max(args.days - 1, 1), 1.0)  # volume grows over the month
    weekday = [1.0, 1.05, 1.1, 1.05, 0.95, 0.55, 0.45][day.weekday()]
    per_day = base_per_day * growth * weekday / 1.1
    for h in range(24):
        lam = per_day * HOUR_SHAPE[h] / sum(HOUR_SHAPE)
        # Poisson draw
        L, k, p = math.exp(-lam), 0, 1.0
        while True:
            p *= rnd.random()
            if p <= L:
                break
            k += 1
        for _ in range(k):
            t = day + dt.timedelta(hours=h, seconds=rnd.uniform(0, 3600))
            if START <= t < END - dt.timedelta(minutes=5):
                created_times.append(t)
created_times.sort()

# --- place each task on a machine (capacity-aware, so machines are busy but never over-booked)
for i, created in enumerate(created_times):
    tpl = rnd.choices(TEMPLATES, weights=TW)[0]
    name, _, ik, cores_c, mem_c, (dlo, dhi), (ulo, uhi), gwk, gwp, (tlo, thi), (glo, ghi), argv, arch = tpl
    cores, mem = rnd.choice(cores_c), rnd.choice(mem_c)
    dur = lognorm(dlo, dhi)
    util = rnd.uniform(ulo, uhi)
    cands = [n for n in nodes if n["arch"] == arch and n["cores"] >= cores and n["mem"] >= mem]
    if name in ("train", "eval"):
        cands = [n for n in cands if n["cores"] >= 8] or cands
    start = created + dt.timedelta(seconds=rnd.uniform(0.6, 4.0))
    node = None
    for _ in range(200):
        free = []
        for n in cands:
            if not online(n, start):
                continue
            n["active"] = [a for a in n["active"] if a[0] > start]
            used_c = sum(a[1] for a in n["active"])
            used_m = sum(a[2] for a in n["active"])
            if n["cores"] - used_c >= cores and n["mem"] - used_m >= mem:
                free.append((n["cores"] - used_c) / n["cores"] + n["bench"] * 0.1 + n["trust"] * 0.3 + rnd.random() * 0.25)
                free[-1] = (free[-1], n)
        if free:
            node = max(free, key=lambda x: x[0])[1]
            break
        # wait for the earliest finish among candidates (or the next minute if all are offline)
        ends = [a[0] for n in cands for a in n["active"]]
        start = min(ends) + dt.timedelta(seconds=1) if ends else start + dt.timedelta(minutes=1)
    if node is None or start + dt.timedelta(seconds=dur) > END:
        continue
    finish = start + dt.timedelta(seconds=dur)
    node["active"].append((finish, cores, mem))
    gw = None
    if gwk and rnd.random() < gwp:
        gw = {"postgres": GW_PG, "metrics": GW_MET, "files": FILES or GW_MET}[gwk]
    tasks.append(dict(i=i, id=mid("tsk"), tpl=name, ik=ik, cores=cores, mem=mem, dur=dur, util=util, start=start, finish=finish, created=created,
                      node=node, gw=gw, to_task=rnd.uniform(tlo, thi) * 1e6 if gw else 0, to_gw=rnd.uniform(glo, ghi) * 1e6 if gw else 0,
                      argv=argv, state="succeeded", code=0, reason="exited", arch=arch, disk=rnd.choice([1024, 2048, 4096, 8192])))

# --- outcomes: about 0.9% fail or are stopped (and exactly under 1% in the last 7 days, where the dashboard measures it)
tasks.sort(key=lambda t: t["finish"])
week_start = NOW - dt.timedelta(days=7)
BAD = [("failed", 137, "oom", 24), ("failed", 1, "exited", 38), ("failed", -1, "wall_timeout", 18), ("cancelled", None, "stopped", 20)]
recent = [t for t in tasks if t["finish"] >= week_start]
want_recent = max(1, int(len(recent) * 0.0087))
older = [t for t in tasks if t["finish"] < week_start]
want_older = int(len(older) * 0.011)
for pool, n in ((recent, want_recent), (older, want_older)):
    for t in rnd.sample(pool, min(n, len(pool))):
        st, code, reason, _ = rnd.choices(BAD, weights=[b[3] for b in BAD])[0]
        t["state"], t["code"], t["reason"] = st, code, reason
        if reason == "oom":
            t["dur"] = max(3, t["dur"] * rnd.uniform(0.15, 0.6))
        elif reason == "wall_timeout":
            t["dur"] = t["dur"] * rnd.uniform(1.0, 1.3)
        elif reason == "stopped":
            t["dur"] = t["dur"] * rnd.uniform(0.1, 0.5)
        else:
            t["dur"] = t["dur"] * rnd.uniform(0.05, 0.9)
        t["finish"] = t["start"] + dt.timedelta(seconds=t["dur"])
        t["to_task"] *= 0.5
n_recent_bad = sum(1 for t in tasks if t["finish"] >= week_start and t["state"] != "succeeded")
n_recent = sum(1 for t in tasks if t["finish"] >= week_start)

# --- derived per-task numbers
for t in tasks:
    t["cores_used"] = t["cores"] * t["util"]
    t["mem_peak"] = int(t["mem"] * 1048576 * rnd.uniform(0.3, 0.85) * (1.0 if t["reason"] != "oom" else 1.15))
    t["price"] = int(round(100 + (50 * t["cores"] + 10 * t["mem"] / 1024 + 2 * t["disk"] / 1024) * t["dur"] * t["node"]["bench"] + (1000 * (t["to_task"] + t["to_gw"]) / 1e9)))
    t["wall"] = next(x for x in (30, 60, 120, 300, 600, 1800, 3600, 7200, 14400) if x >= t["dur"] * 1.6)

# ------------------------------------------------------------------------------------------------------------------ SQL
w("BEGIN;")
d = args.shift_old_days
w(f"UPDATE tasks SET created_at = created_at - interval '{d} days', started_at = started_at - interval '{d} days', finished_at = finished_at - interval '{d} days' "
  f"WHERE account_id = {CUST} AND id NOT LIKE '%{TAG}%' AND created_at > now() - interval '{d - 5} days';")
w(f"UPDATE ledger_entries SET created_at = created_at - interval '{d} days' WHERE task_id IN (SELECT id FROM tasks WHERE account_id = {CUST} AND id NOT LIKE '%{TAG}%' AND created_at < now() - interval '{d - 5} days') AND created_at > now() - interval '{d - 5} days';")
w(f"UPDATE task_meters SET started_at = started_at - interval '{d} days', finished_at = finished_at - interval '{d} days' WHERE task_id IN (SELECT id FROM tasks WHERE account_id = {CUST} AND id NOT LIKE '%{TAG}%' AND created_at < now() - interval '{d - 5} days') AND started_at > now() - interval '{d - 5} days';")

for n in nodes:
    w(f"INSERT INTO nodes (id, account_id, hostname, arch, capabilities, offer_cores, offer_memory_mb, offer_disk_mb, offer_network_mbps, bench_score, trust_score, connected, last_heartbeat_at, created_at, instance_id, agent_version) "
      f"VALUES ({q(n['id'])}, {PROV}, {q(n['host'])}, {q(n['arch'])}, '{{}}'::jsonb, {n['cores']}, {n['mem']}, {n['disk']}, {n['mbps']}, {n['bench']}, {n['trust']}, {'true' if online(n, NOW) else 'false'}, {ts(NOW)}, {ts(n['created'])}, {q('ins_' + n['id'][4:])}, 'v0.4.26');")
for g in gateways:
    if g["demo"]:
        w(f"INSERT INTO gateways (id, account_id, label, services, connected, created_at, version, max_mbps) VALUES ({q(g['id'])}, {CUST}, {q(g['label'])}, {jsonb([{'name': g['service'], 'port': g['port']}])}, true, {ts(NOW - dt.timedelta(days=rnd.randint(25, 38)))}, 'v0.4.26', {g['mbps']});")

BATCH = 400


def flush(prefix, rows, suffix=""):
    for i in range(0, len(rows), BATCH):
        w(prefix + ",\n".join(rows[i:i + BATCH]) + suffix + ";")


rows = []
for t in tasks:
    gwj = [{"gateway_id": t["gw"]["id"], "hostname": t["gw"]["service"], "port": t["gw"]["port"]}] if t["gw"] else []
    date = t["created"].strftime("%Y-%m-%d")
    av = t["argv"](date, t["i"])
    ent = ["sh", "-c"] if t["ik"] in ("alpine",) or (av and av[0] == "sh") else None
    if av and av[0] == "sh":
        ent, av = ["sh", "-c"], av[2:]
    code = "NULL" if t["code"] is None else t["code"]
    rows.append(f"({q(t['id'])}, {CUST}, {q(t['state'])}::task_state, {q(IMG[t['ik']])}, {arr(ent)}, {arr(av)}, '{{}}'::jsonb, '', {t['cores']}, {t['mem']}, {t['disk']}, {t['wall']}, {q(t['arch'])}, 'podman', "
                f"{arr([t['gw']['id']]) if t['gw'] else 'ARRAY[]::text[]'}, 'at_most_once', 1, 1, {q(t['node']['id'])}, {code}, {q(t['reason'])}, {ts(t['start'])}, {ts(t['finish'])}, {ts(t['created'])}, {jsonb(gwj)})")
flush("INSERT INTO tasks (id, account_id, state, image, entrypoint, args, env, workdir, cpu_cores, memory_mb, disk_mb, wall_timeout_s, req_arch, req_isolation, gateway_ids, delivery, max_attempts, attempt, node_id, exit_code, exit_reason, started_at, finished_at, created_at, tunnel_targets) VALUES\n", rows)

rows = [f"({q(t['id'])}, {q(t['node']['id'])}, {ts(t['start'])}, {ts(t['finish'])}, {t['dur']:.3f}, {t['dur'] * t['node']['bench']:.3f})" for t in tasks if t["state"] != "cancelled"]
flush("INSERT INTO task_meters (task_id, node_id, started_at, finished_at, duration_s, normalised_s) VALUES\n", rows)

rows, rows2 = [], []
for t in tasks:
    if t["state"] == "cancelled":
        t["price"] = max(int(t["price"] * 0.4), 100)
    rows.append(f"({q('led_' + t['id'][4:])}, {q(t['id'])}, {CUST}, 'charge', {-t['price']}, {ts(t['finish'])})")
    rows2.append(f"({q('ldp_' + t['id'][4:])}, {q(t['id'])}, {PROV}, 'credit', {t['price']}, {ts(t['finish'])})")
flush("INSERT INTO ledger_entries (id, task_id, account_id, kind, amount_micros, created_at) VALUES\n", rows)
flush("INSERT INTO ledger_entries (id, task_id, account_id, kind, amount_micros, created_at) VALUES\n", rows2)
charges = sum(t["price"] for t in tasks)
w(f"UPDATE accounts SET balance_micros = {int(args.balance * 1e6) - charges} WHERE id = {CUST};")
w(f"UPDATE accounts SET balance_micros = balance_micros + {charges} WHERE id = {PROV};")

rows = [f"({q(t['id'])}, {q(t['node']['id'])}, {t['cores_used'] * t['dur']:.2f}, {t['mem_peak']}, {int(t['to_gw'])}, {int(t['to_task'])}, {max(int(t['dur'] / 15), 1)}, {ts(t['finish'])})" for t in tasks]
flush("INSERT INTO task_usage_totals (task_id, node_id, core_seconds, peak_memory_bytes, tunnel_to_gateway, tunnel_to_task, samples, updated_at) VALUES\n", rows)

# --- per-task usage samples (the task page's Resource use chart) and a few log lines, for the most recent tasks
rows, logrows = [], []
for t in tasks:
    if t["finish"] < NOW - dt.timedelta(days=3):
        continue
    steps = max(int(t["dur"] / 15), 2)
    for s in range(steps):
        at = t["start"] + dt.timedelta(seconds=min((s + 1) * 15, t["dur"]))
        ramp = min(1.0, (s + 1) / 3)
        cpu = t["cores_used"] * ramp * rnd.uniform(0.9, 1.08)
        mem = int(t["mem_peak"] * min(1.0, 0.35 + 0.65 * (s + 1) / steps))
        out_b = int(t["to_gw"] / steps) if t["gw"] else 0
        in_b = int(t["to_task"] / steps) if t["gw"] else 0
        rows.append(f"({q(t['id'])}, {ts(at)}, {cpu:.4f}, {mem}, {out_b}, {in_b})")
    if t["finish"] >= NOW - dt.timedelta(hours=36):
        lines = [f"{t['start'].strftime('%Y-%m-%dT%H:%M:%SZ')} INFO  {t['tpl']} starting"]
        if t["gw"]:
            lines.append(f"{(t['start'] + dt.timedelta(seconds=1)).strftime('%Y-%m-%dT%H:%M:%SZ')} INFO  connected to {t['gw']['service']}:{t['gw']['port']} through the gateway")
        if t["state"] == "failed" and t["reason"] == "exited":
            lines.append(f"{t['finish'].strftime('%Y-%m-%dT%H:%M:%SZ')} ERROR step failed: upstream returned 503, giving up after 3 attempts")
        elif t["state"] == "succeeded":
            lines.append(f"{t['finish'].strftime('%Y-%m-%dT%H:%M:%SZ')} INFO  {t['tpl']} finished ok in {t['dur']:.0f}s")
        for k, line in enumerate(lines, 1):
            stream = "stderr" if "ERROR" in line else "stdout"
            logrows.append(f"({q(t['id'])}, {k}, {q(stream)}, {ts(t['start'] + dt.timedelta(seconds=k))}, {q(line)})")
flush("INSERT INTO task_usage_samples (task_id, at, cpu_cores, memory_bytes, tunnel_out, tunnel_in) VALUES\n", rows, "\nON CONFLICT DO NOTHING")
flush("INSERT INTO task_logs (task_id, seq, stream, at, line) VALUES\n", logrows, "\nON CONFLICT DO NOTHING")

# --- machine usage: 5-minute buckets for 10 days, per task shares for 8 days, 30-second samples for the last 2 hours
BUCKET = dt.timedelta(minutes=5)
usage_from = NOW - dt.timedelta(days=10)
task_from = NOW - dt.timedelta(days=8)
OS_MEM = 2.4 * 2**30


def floor5(t):
    return t.replace(minute=t.minute - t.minute % 5, second=0)


by_node = {n["id"]: [] for n in nodes}
for t in tasks:
    by_node[t["node"]["id"]].append(t)
node_rows, ntu_rows, sample_rows = [], [], []
latest = {}
for n in nodes:
    disk_total = n["disk"] * 1048576
    mem_total = int(n["mem"] * 1048576 * 1.25)
    ts_list = sorted(by_node[n["id"]], key=lambda x: x["start"])
    b = floor5(usage_from)
    j = 0
    while b + BUCKET <= NOW - dt.timedelta(minutes=3):
        if online(n, b + BUCKET / 2):
            active = []
            for t in ts_list:
                if t["start"] >= b + BUCKET:
                    break
                if t["finish"] > b:
                    active.append(t)
            cpu_t = mem_t = 0.0
            for t in active:
                ov = (min(t["finish"], b + BUCKET) - max(t["start"], b)).total_seconds()
                share = max(ov, 0) / 300.0
                cpu_t += t["cores_used"] * share
                mem_t += t["mem_peak"] * share
                if b >= task_from and ov > 0:
                    smp = ov / 30.0
                    frac = ov / max(t["dur"], 1)
                    ntu_rows.append(f"({q(n['id'])}, {q(t['id'])}, {ts(b)}, {t['cores_used'] * smp:.3f}, {t['mem_peak'] * smp:.0f}, {int(t['to_gw'] * frac)}, {int(t['to_task'] * frac)})")
            busy = min(0.985, cpu_t / n["cores"] * 1.04 + 0.012 + rnd.uniform(0, 0.012))
            mem_used = min(mem_total * 0.96, OS_MEM + mem_t * 1.05)
            hour = b.hour + b.minute / 60
            disk_used = disk_total * (0.34 + 0.05 * math.sin(((b - START).total_seconds() / 86400) / 3.1) + 0.012 * math.sin(hour / 24 * 6.283))
            node_rows.append(f"({q(n['id'])}, {ts(b)}, 10, {busy * 10:.4f}, {n['cores']}, {mem_used * 10:.0f}, {mem_total}, {disk_used * 10:.0f}, {disk_total}, {cpu_t * 10:.4f}, {mem_t * 10:.0f})")
            latest[n["id"]] = (busy, mem_used, disk_used)
        b += BUCKET
    # 30-second samples for the last 2 hours
    s = (NOW - dt.timedelta(hours=2)).replace(second=0, microsecond=0)
    while s <= NOW - dt.timedelta(minutes=3):
        if online(n, s):
            act = [t for t in ts_list if t["start"] <= s < t["finish"]]
            cpu_t = sum(t["cores_used"] * rnd.uniform(0.93, 1.07) for t in act)
            mem_t = sum(t["mem_peak"] for t in act)
            busy = min(0.985, cpu_t / n["cores"] * 1.04 + 0.012 + rnd.uniform(0, 0.012))
            tj = [{"id": t["id"], "cpu": round(t["cores_used"], 3), "mem": t["mem_peak"], "out": int(t["to_gw"] / max(t["dur"] / 30, 1)), "in": int(t["to_task"] / max(t["dur"] / 30, 1))} for t in act]
            sample_rows.append(f"({q(n['id'])}, {ts(s)}, {busy:.4f}, {n['cores']}, {int(OS_MEM + mem_t)}, {mem_total}, {int(disk_total * 0.36)}, {disk_total}, {cpu_t:.4f}, {int(mem_t)}, {jsonb(tj)})")
        s += dt.timedelta(seconds=30)
flush("INSERT INTO node_usage (node_id, bucket, samples, cpu_busy_sum, cpu_count, mem_used_sum, mem_total, disk_used_sum, disk_total, tasks_cpu_sum, tasks_mem_sum) VALUES\n", node_rows, "\nON CONFLICT DO NOTHING")
flush("INSERT INTO node_task_usage (node_id, task_id, bucket, cpu_sum, mem_sum, tunnel_out, tunnel_in) VALUES\n", ntu_rows, "\nON CONFLICT DO NOTHING")
flush("INSERT INTO node_usage_samples (node_id, at, host_cpu_busy, cpu_count, mem_used, mem_total, disk_used, disk_total, tasks_cpu, tasks_mem, tasks) VALUES\n", sample_rows, "\nON CONFLICT DO NOTHING")
for n in nodes:
    if n["id"] in latest:
        busy, mem_used, disk_used = latest[n["id"]]
        sample = {"At": NOW.strftime("%Y-%m-%dT%H:%M:%SZ"), "Tasks": None, "NodeID": n["id"], "DiskUsed": int(disk_used), "DiskTotal": n["disk"] * 1048576, "IntervalMS": 15000,
                  "HostCPUBusy": round(busy, 4), "HostMemUsed": int(mem_used), "HostCPUCount": n["cores"], "HostMemTotal": int(n["mem"] * 1048576 * 1.25)}
        w(f"INSERT INTO node_usage_latest (node_id, at, sample) VALUES ({q(n['id'])}, {ts(NOW)}, {jsonb(sample)}) ON CONFLICT (node_id) DO UPDATE SET at = EXCLUDED.at, sample = EXCLUDED.sample;")

# --- gateway traffic: the task's bytes spread over the 5-minute buckets it ran in
rows, tot = [], {}
for t in tasks:
    if not t["gw"]:
        continue
    g = t["gw"]
    b = floor5(t["start"])
    spans = []
    while b < t["finish"]:
        ov = (min(t["finish"], b + BUCKET) - max(t["start"], b)).total_seconds()
        if ov > 0:
            spans.append((b, ov))
        b += BUCKET
    total_ov = sum(o for _, o in spans) or 1
    conns = max(1, int(t["dur"] / 20) + rnd.randint(0, 3))
    for bb, ov in spans:
        f = ov / total_ov
        rows.append(f"({q(g['id'])}, {q(t['id'])}, {q(g['service'])}, {ts(bb)}, {int(t['to_gw'] * f)}, {int(t['to_task'] * f)}, {max(1, int(conns * f))})")
    a = tot.setdefault(g["id"], [0, 0, 0])
    a[0] += int(t["to_gw"]); a[1] += int(t["to_task"]); a[2] += conns
    w(f"INSERT INTO task_gateway_totals (task_id, gateway_id, service, bytes_to_local, bytes_to_task, connections) VALUES ({q(t['id'])}, {q(g['id'])}, {q(g['service'])}, {int(t['to_gw'])}, {int(t['to_task'])}, {conns}) ON CONFLICT DO NOTHING;")
flush("INSERT INTO gateway_traffic (gateway_id, task_id, service, bucket, bytes_to_local, bytes_to_task, connections) VALUES\n", rows, "\nON CONFLICT DO NOTHING")
for gid, (l, tt, c) in tot.items():
    w(f"INSERT INTO gateway_totals (gateway_id, bytes_to_local, bytes_to_task, connections) VALUES ({q(gid)}, {l}, {tt}, {c}) ON CONFLICT (gateway_id) DO UPDATE SET bytes_to_local = gateway_totals.bytes_to_local + {l}, bytes_to_task = gateway_totals.bytes_to_task + {tt}, connections = gateway_totals.connections + {c}, updated_at = now();")
w("COMMIT;")

import sys
print(f"-- tasks {len(tasks)}  last7d {n_recent} (bad {n_recent_bad} = {100 * n_recent_bad / max(n_recent, 1):.2f}%)  avg fleet util target {TARGET_UTIL:.0%}  charges ${charges / 1e6:,.2f}", file=sys.stderr)
print("\n".join(out))
