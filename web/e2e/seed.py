#!/usr/bin/env python3
"""Prints SQL that fills a local LazyCake database with realistic demo data, for
designing and screenshotting the portals against something that looks like a
real account instead of an empty one.

Run after the coordinator has started once with LAZYCAKE_SEED_PORTAL_* set (that
creates the `alice` customer and `bob` provider portal accounts this attaches
data to):

    python3 web/e2e/seed.py | psql "$LAZYCAKE_DATABASE_URL"

Deliberately includes the awkward cases the UI must survive: 30-character task
IDs, a 71-character digest-pinned image, a very long hostname, every task
state, a failed task with stderr, a running task with a long log, an empty
account's worth of "nothing yet" (the second customer, `carol`, has no data).
"""
import hashlib
import random

random.seed(7)
ALICE = "(SELECT account_id FROM portal_credentials WHERE username='alice')"
BOB = "(SELECT account_id FROM portal_credentials WHERE username='bob')"

def ulid(tag: str) -> str:
    h = hashlib.md5(tag.encode()).hexdigest().upper()
    return ("01M4" + h)[:26]

def q(s):  # SQL string literal
    return "'" + str(s).replace("'", "''") + "'"

IMG_LCBENCH = "docker.io/fattymango/lcbench@sha256:65e8c5765cc43af8cf9e6a71165d31954dbfee495aa1ada997fb35b8c672e1db"
IMG_ALPINE = "docker.io/library/alpine@sha256:c64c687cbea9300178b30c95835354e34c4e4febc4badfe27102879de0483b5e"
IMG_PY = "docker.io/library/python@sha256:2f3e8a1b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f7"
IMG_REF = "localhost/refworkload@sha256:9a7d1c0e3b5f2a4c6e8d0b1a3c5e7f9a2b4d6c8e0f1a3b5c7d9e1f2a4b6c8d0e"

out = []
w = out.append

w("BEGIN;")
# Safe to rerun: clears everything this script creates (never the portal
# credentials or accounts). Only ever point this at a throwaway demo database.
w("TRUNCATE task_logs, ledger_entries, task_meters, tasks, gateways, nodes CASCADE;")
w("UPDATE accounts SET balance_micros = 24317500 WHERE id = " + ALICE + ";")

# --- provider bob's machines -------------------------------------------------
nodes = [
    ("nod_" + ulid("n1"), "kassab-workstation-ubuntu-24-04", "amd64", 8, 16384, 200000, 0.94, True, 0.4),
    ("nod_" + ulid("n2"), "atlas-rack-07.fra1.internal.example-datacenter.net", "amd64", 32, 131072, 900000, 0.81, True, 1.1),
    ("nod_" + ulid("n3"), "pi-cluster-node-3", "arm64", 4, 8192, 64000, 0.52, False, 5400),
    ("nod_" + ulid("n4"), "lab-laptop", "amd64", 2, 4096, 32000, 0.33, False, 172800),
]
for nid, host, arch, cores, mem, disk, trust, conn, hb_ago in nodes:
    w(f"INSERT INTO nodes (id, account_id, hostname, arch, offer_cores, offer_memory_mb, offer_disk_mb, bench_score, trust_score, connected, last_heartbeat_at, created_at) "
      f"VALUES ({q(nid)}, {BOB}, {q(host)}, {q(arch)}, {cores}, {mem}, {disk}, {round(random.uniform(0.8,1.2),3)}, {trust}, {str(conn).lower()}, now() - interval '{hb_ago} seconds', now() - interval '{random.randint(3,40)} days');")

# --- alice's gateways --------------------------------------------------------
gws = [
    ("gw_" + ulid("g1"), "production-postgres", True, [{"name": "postgres", "port": 5432}]),
    ("gw_" + ulid("g2"), "internal-files", True, [{"name": "files", "port": 8000}, {"name": "metrics", "port": 9100}]),
    ("gw_" + ulid("g3"), "staging-api-gateway-eu-west", False, [{"name": "api", "port": 443}]),
]
import json
for gid, label, conn, svcs in gws:
    w(f"INSERT INTO gateways (id, account_id, label, services, connected, created_at) VALUES ({q(gid)}, {ALICE}, {q(label)}, {q(json.dumps(svcs))}::jsonb, {str(conn).lower()}, now() - interval '{random.randint(2,30)} days');")

# --- alice's tasks -----------------------------------------------------------
NODE_IDS = [n[0] for n in nodes]
tasks = []
def task(tag, state, image, args, cores, mem, age_min, dur_s=None, node=None, exit_code=None, reason=None, tunnels=None, env=None):
    tid = "tsk_" + ulid(tag)
    tasks.append((tid, state))
    started = f"now() - interval '{age_min*60 - 20} seconds'" if state not in ("queued",) else "NULL"
    finished = f"now() - interval '{max(age_min*60 - 20 - (dur_s or 0), 1)} seconds'" if dur_s is not None else "NULL"
    arr = "ARRAY[" + ",".join(q(a) for a in args) + "]::text[]" if args else "NULL"
    node_sql = q(node) if node else "NULL"
    ec = exit_code if exit_code is not None else "NULL"
    rs = q(reason) if reason else "NULL"
    tj = q(json.dumps(tunnels or []))
    ej = q(json.dumps(env or {}))
    # req_arch riscv64: no seeded node can claim it, so a local scheduler
    # leaves queued tasks queued instead of dispatching them to nodes that
    # have no agent behind them.
    arch = "riscv64" if state == "queued" else "amd64"
    w(f"INSERT INTO tasks (id, account_id, state, image, args, env, workdir, req_arch, cpu_cores, memory_mb, disk_mb, wall_timeout_s, node_id, exit_code, exit_reason, started_at, finished_at, created_at, tunnel_targets) "
      f"VALUES ({q(tid)}, {ALICE}, {q(state)}, {q(image)}, {arr}, {ej}::jsonb, '', {q(arch)}, {cores}, {mem}, 1024, 600, {node_sql}, {ec}, {rs}, {started}, {finished}, now() - interval '{age_min} minutes', {tj}::jsonb);")
    return tid

# The newest running task: the one with the long log, so e2e tests that open "a running task" get one with output.
RUNNING = task("running-demo", "running", IMG_LCBENCH, None, 0.5, 512, 2, env={"LCBENCH_DURATION": "5m"}, node=NODE_IDS[0])
FAILED = task("failed-demo", "failed", IMG_PY, ["python", "-m", "etl.load", "--source", "postgres.prod", "--batch-size", "5000"], 1, 1024, 95, dur_s=48, node=NODE_IDS[1], exit_code=1, reason="exited",
              tunnels=[{"gateway_id": gws[0][0], "hostname": "postgres", "port": 5432}])
task("queued-1", "queued", IMG_ALPINE, ["sleep", "300"], 0.25, 128, 1)
task("queued-2", "queued", IMG_REF, None, 2, 2048, 2)
task("dispatched-1", "dispatched", IMG_LCBENCH, None, 1, 512, 3, node=NODE_IDS[1])
task("running-2", "running", IMG_REF, None, 4, 4096, 12, node=NODE_IDS[1])
task("running-3", "running", IMG_ALPINE, ["wget", "-qO-", "http://files:8000/test.txt"], 0.5, 128, 4, node=NODE_IDS[0],
     tunnels=[{"gateway_id": gws[1][0], "hostname": "files", "port": 8000}])
task("oom", "failed", IMG_PY, ["python", "train.py"], 2, 512, 210, dur_s=31, node=NODE_IDS[0], exit_code=137, reason="oom")
task("timeout", "failed", IMG_ALPINE, ["sleep", "3600"], 0.5, 128, 320, dur_s=600, node=NODE_IDS[1], exit_code=-1, reason="wall_timeout")
task("fenced", "fenced", IMG_REF, None, 2, 2048, 400, dur_s=180, node=NODE_IDS[2], reason="fenced")
task("abandoned", "abandoned", IMG_ALPINE, ["true"], 0.25, 128, 700, reason="abandoned")
task("cancelled", "cancelled", IMG_ALPINE, ["sleep", "60"], 0.25, 128, 520)
for i in range(16):
    img = random.choice([IMG_LCBENCH, IMG_ALPINE, IMG_REF, IMG_PY])
    task(f"ok-{i}", "succeeded", img, random.choice([None, ["echo", "hello"], ["sh", "-c", "sleep 5"]]),
         random.choice([0.25, 0.5, 1, 2]), random.choice([128, 256, 512, 1024]),
         random.randint(30, 2600), dur_s=random.randint(8, 240), node=random.choice(NODE_IDS[:3]), exit_code=0, reason="exited")

# --- money: charges on alice, credits for bob --------------------------------
w("INSERT INTO task_meters (task_id, node_id, started_at, finished_at, duration_s, normalised_s) "
  "SELECT id, node_id, started_at, finished_at, EXTRACT(EPOCH FROM finished_at-started_at), EXTRACT(EPOCH FROM finished_at-started_at) FROM tasks "
  "WHERE state IN ('succeeded','failed','fenced') AND node_id IS NOT NULL AND started_at IS NOT NULL AND finished_at IS NOT NULL;")
w(f"INSERT INTO ledger_entries (id, task_id, account_id, kind, amount_micros, created_at) "
  f"SELECT 'led_' || substr(md5(id || 'c'),1,22), id, account_id, 'charge', (cpu_cores * 4200 + memory_mb * 6)::bigint, finished_at FROM tasks WHERE state IN ('succeeded','failed') AND finished_at IS NOT NULL;")
w(f"INSERT INTO ledger_entries (id, task_id, account_id, kind, amount_micros, created_at) "
  f"SELECT 'led_' || substr(md5(t.id || 'p'),1,22), t.id, n.account_id, 'credit', ((t.cpu_cores * 4200 + t.memory_mb * 6) * 0.85)::bigint, t.finished_at "
  f"FROM tasks t JOIN nodes n ON n.id = t.node_id WHERE t.state IN ('succeeded','failed') AND t.finished_at IS NOT NULL;")

# --- logs ---------------------------------------------------------------------
def logs(tid, lines):
    vals = []
    for i, (stream, text) in enumerate(lines, 1):
        vals.append(f"({q(tid)}, {i}, {q(stream)}, now() - interval '{max(len(lines)-i,0)*2 + 1} seconds', {q(text)})")
    w("INSERT INTO task_logs (task_id, seq, stream, at, line) VALUES " + ",".join(vals) + ";")

logs(FAILED, [
    ("stdout", "2026-10-06T17:21:04Z INFO  etl.load starting source=postgres.prod batch_size=5000"),
    ("stdout", "2026-10-06T17:21:04Z INFO  resolving postgres:5432 through gateway tunnel"),
    ("stdout", "2026-10-06T17:21:05Z INFO  connected, server version 16.4"),
    ("stdout", "2026-10-06T17:21:05Z INFO  extracting table public.orders (est. 2,418,330 rows)"),
    ("stdout", "2026-10-06T17:21:21Z INFO  batch 1/484 ok (5000 rows, 15.8s)"),
    ("stdout", "2026-10-06T17:21:36Z INFO  batch 2/484 ok (5000 rows, 15.1s)"),
    ("stderr", "2026-10-06T17:21:52Z WARN  slow query on batch 3: 31.4s (threshold 20s)"),
    ("stderr", "Traceback (most recent call last):"),
    ("stderr", '  File "/app/etl/load.py", line 142, in <module>'),
    ("stderr", "    main()"),
    ("stderr", '  File "/app/etl/load.py", line 118, in main'),
    ("stderr", "    cursor.execute(QUERY, params)"),
    ("stderr", '  File "/usr/local/lib/python3.12/site-packages/psycopg/cursor.py", line 723, in execute'),
    ("stderr", "    raise ex.with_traceback(None)"),
    ("stderr", 'psycopg.errors.QueryCanceled: canceling statement due to statement timeout'),
    ("stderr", "CONTEXT:  while fetching rows from cursor \"c_0x7f3a9c1e\""),
    ("stdout", "2026-10-06T17:22:04Z ERROR load failed after 2 of 484 batches; exiting with status 1"),
])
run_lines = [("stdout", "lcbench start: duration=5m0s workers=8 host_cores=8 cpu_limit=0.50 mem_limit=512MB mem_cap=460MB")]
for t in range(1, 150):
    cpu = round(random.uniform(0.44, 0.56), 2)
    mem = min(18 + t * 3, 453)
    run_lines.append(("stdout", f"t={t:3d}s cpu_used={cpu:.2f} cores (limit 0.50) throttled_periods={t*10 + random.randint(0,3)} | mem_used={mem}MB (limit 512MB) allocated={min(16*t, 448)}MB | loops={t*980 + random.randint(0,200)}"))
logs(RUNNING, run_lines)

w("COMMIT;")
print("\n".join(out))
