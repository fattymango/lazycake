#!/usr/bin/env python3
"""Production load test for LazyCake: many tasks of many sizes (CPU, memory, network through both gateways,
failures, cancellations, bursts) for about an hour, with safety stops, and a report at the end.

Everything is submitted as the customer account `kassab` through the portal API, the same way a person would.
Safety: stops submitting if the coordinator stops answering, the server disk runs low, the balance runs low,
or the in-flight cap is reached. Run with:  python3 loadtest.py
"""
import http.cookiejar, json, os, random, statistics, subprocess, sys, time, urllib.error, urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
BASE = os.environ.get("LAZYCAKE_BASE", "http://127.0.0.1:8080")
SERVER = os.environ.get("LAZYCAKE_SSH", "")  # user@host of the coordinator, for server health probes (optional)
OUT = os.environ.get("LAZYCAKE_LOADTEST_OUT", HERE)
LOG = open(os.path.join(OUT, "loadtest.log"), "a", buffering=1)
EVENTS = open(os.path.join(OUT, "loadtest-events.jsonl"), "a", buffering=1)
random.seed(20261007)

ALPINE = "docker.io/library/alpine@sha256:c64c687cbea9300178b30c95835354e34c4e4febc4badfe27102879de0483b5e"
LCBENCH = "docker.io/fattymango/lcbench@sha256:65e8c5765cc43af8cf9e6a71165d31954dbfee495aa1ada997fb35b8c672e1db"
GW_TEST = os.environ["LAZYCAKE_GW_SMALL"]  # a gateway whose service "testfile":8000 serves a tiny file at /test.txt
GW_VM = os.environ["LAZYCAKE_GW_BIG"]  # a gateway whose service "files":8000 serves / and a 50 MB /loadtest-50MB.bin

RUN_MINUTES = 50  # stop submitting after this
DRAIN_MINUTES = 12  # then wait this long for everything to finish
TICK = 10  # seconds between rounds
IN_FLIGHT = ("queued", "reserved", "dispatched", "running")


def log(msg):
    line = f"{time.strftime('%H:%M:%S')} {msg}"
    print(line, flush=True)
    LOG.write(line + "\n")


# --------------------------------------------------------------------------------------------- API
jar = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
PASSWORD = os.environ["LAZYCAKE_PASSWORD"]
USERNAME = os.environ.get("LAZYCAKE_USER", "kassab")


def raw(method, path, body=None, timeout=30):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method, headers={"content-type": "application/json"})
    with opener.open(req, timeout=timeout) as r:
        txt = r.read()
        return r.status, (json.loads(txt) if txt else None)


def login():
    raw("POST", "/api/portal/login", {"username": USERNAME, "password": PASSWORD})


def call(method, path, body=None):
    for attempt in (1, 2):
        try:
            return raw(method, path, body)
        except urllib.error.HTTPError as e:
            if e.code == 401 and attempt == 1:
                login()
                continue
            try:
                return e.code, json.loads(e.read() or b"null")
            except Exception:
                return e.code, None
        except Exception as e:  # network trouble
            return 0, {"error": str(e)}
    return 0, None


# --------------------------------------------------------------------------------------------- task kinds
def task(args, cores, mem, timeout, image=ALPINE, env=None, targets=None, entry=("sh", "-c")):
    body = {"image": image, "cores": cores, "memory_mb": mem, "wall_timeout_s": timeout}
    if image == ALPINE:
        body["entrypoint"] = list(entry)
        body["args"] = [args]
    if env:
        body["env"] = env
    if targets:
        body["tunnel_targets"] = targets
    return body


BOTH = [{"gateway_id": GW_TEST, "hostname": "testfile", "port": 8000}, {"gateway_id": GW_VM, "hostname": "files", "port": 8000}]
VMONLY = [{"gateway_id": GW_VM, "hostname": "files", "port": 8000}]


def net_loop(seconds, parallel):
    loop = (
        "( end=$(( $(date +%s) + {s} )); while [ $(date +%s) -lt $end ]; do "
        "wget -q -O /dev/null http://testfile:8000/test.txt; wget -q -O /dev/null http://files:8000/; done ) &"
    ).format(s=seconds)
    return "\n".join([loop] * parallel) + f"\nsleep {seconds + 2}\necho net-done"


def mk(kind):
    if kind == "hello":
        return task('echo "hello from the load test"', 0.25, 64, 60)
    if kind == "bench_s":
        return task("", 0.5, 256, 150, LCBENCH, {"LCBENCH_DURATION": "30s"})
    if kind == "bench_m":
        return task("", 1, 512, 200, LCBENCH, {"LCBENCH_DURATION": "60s"})
    if kind == "bench_l":
        return task("", 2, 1024, 240, LCBENCH, {"LCBENCH_DURATION": "90s"})
    if kind == "bench_xl":
        return task("", 4, 2048, 240, LCBENCH, {"LCBENCH_DURATION": "60s"})
    if kind == "bench_xxl":
        return task("", 8, 4096, 240, LCBENCH, {"LCBENCH_DURATION": "60s"})
    if kind == "net_small":
        s = random.choice([40, 60, 90])
        return task(net_loop(s, random.choice([2, 3, 4])), 0.25, 128, s + 60, targets=BOTH)
    if kind == "net_big":
        return task(
            "for i in 1 2; do wget -q -O /dev/null http://files:8000/loadtest-50MB.bin || echo FAIL; done; echo big-done",
            0.5, 128, 400, targets=VMONLY,
        )
    if kind == "mixed_max":
        script = (
            "awk 'BEGIN{s=\"x\";for(i=0;i<25;i++)s=s s;for(k=0;k<8;k++)a[k]=s k;system(\"sleep 150\")}' &\n"
            "yes > /dev/null &\nyes > /dev/null &\n"
            + net_loop(145, 3)
            + "\n"
        )
        return task(script, 1, 512, 260, targets=BOTH)
    if kind == "oom":  # asks for far more memory than its 128 MB limit: must be killed, not allowed to take the machine
        return task("awk 'BEGIN{s=\"x\";while(1){s=s s}}'", 0.25, 128, 90)
    if kind == "timeout_t":  # runs past its wall-clock limit: must be stopped by the platform
        return task("sleep 300", 0.25, 64, 20)
    if kind == "exit_fail":
        return task("echo failing on purpose; exit 3", 0.25, 64, 60)
    if kind == "bad_image":  # a digest that doesn't exist: the pull must fail cleanly
        return task("echo never", 0.25, 64, 60, image="docker.io/library/alpine@sha256:" + "0" * 64)
    if kind == "sleeper":  # long enough to be cancelled while running
        return task("sleep 150; echo woke", 0.25, 64, 200)
    raise ValueError(kind)


# kind -> weight, per phase (start minute, end minute, per-tick submissions, in-flight cap, weights)
PHASES = [
    (0, 6, 4, 150, {"hello": 30, "bench_s": 30, "bench_m": 15, "net_small": 25}),
    (6, 18, 6, 150, {"hello": 15, "bench_s": 10, "bench_m": 20, "bench_l": 10, "net_small": 25, "net_big": 15, "mixed_max": 5}),
    (18, 32, 8, 150, {"bench_l": 15, "bench_xl": 10, "bench_xxl": 5, "mixed_max": 15, "net_big": 15, "net_small": 15, "oom": 5,
                      "timeout_t": 5, "exit_fail": 5, "bad_image": 3, "sleeper": 7}),
    (32, 46, 25, 300, {"hello": 50, "bench_s": 20, "net_small": 20, "sleeper": 10}),
    (46, 50, 6, 150, {"bench_m": 30, "net_small": 30, "mixed_max": 20, "hello": 20}),
]
EXPECTED_FAILURES = {"oom", "timeout_t", "exit_fail", "bad_image"}


def phase_at(minute):
    for p in PHASES:
        if p[0] <= minute < p[1]:
            return p
    return None


# --------------------------------------------------------------------------------------------- server probes
def ssh(cmd, timeout=40):
    try:
        out = subprocess.run(["ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=8", SERVER, cmd], capture_output=True, text=True, timeout=timeout)
        return out.stdout.strip()
    except Exception as e:
        return f"ssh-error: {e}"


def server_probe():
    if not SERVER:
        return {"active": "active"}
    out = ssh(
        "echo $(systemctl is-active lazycake-coordinator) "
        "$(systemctl show lazycake-coordinator -p MemoryCurrent --value) "
        "$(df --output=avail -m / | tail -1 | tr -d ' ') "
        "$(awk '{print $1}' /proc/loadavg) "
        "$(free -m | awk 'NR==2{print $7}')"
    ).split()
    try:
        return {"active": out[0], "rss_mb": round(int(out[1]) / 1048576, 1), "disk_free_mb": int(out[2]), "load1": float(out[3]), "avail_mb": int(out[4])}
    except Exception:
        return {"active": "unknown", "raw": out}


def db(sql):
    if not SERVER:
        return ""
    return ssh(f"podman exec lazycake-postgres psql -U lazycake -d lazycake -tAF'|' -c \"{sql}\"", timeout=60)


def snapshot(label):
    nodes = db("select right(id,8), hostname, connected, round(trust_score::numeric,3), offer_cores, offer_memory_mb from nodes where connected or last_heartbeat_at > now() - interval '3 hours' order by created_at")
    size = db("select pg_size_pretty(pg_database_size('lazycake'))")
    log(f"[{label}] nodes (id|host|connected|trust|cores|mem):\n{nodes}\n[{label}] database size: {size}")
    return nodes


def sql_states(start):
    out = db(f"select state, count(*) from tasks where created_at >= to_timestamp({start}) group by 1")
    res = {}
    for line in out.splitlines():
        if "|" in line:
            k, v = line.split("|")
            res[k] = int(v)
    return res


def sql_tasks(start):
    out = db(
        "select id, state, coalesce(exit_reason,''), coalesce(node_id,''), (extract(epoch from created_at)*1000)::bigint, "
        "coalesce((extract(epoch from started_at)*1000)::bigint,0), coalesce((extract(epoch from finished_at)*1000)::bigint,0) "
        f"from tasks where created_at >= to_timestamp({start}) and account_id = (select id from accounts where name = '{USERNAME}' limit 1)"
    )
    rows = []
    for line in out.splitlines():
        f = line.split("|")
        if len(f) == 7:
            rows.append({"id": f[0], "state": f[1], "exit_reason": f[2], "node_id": f[3], "created_at_ms": int(f[4]),
                         "started_at_ms": int(f[5]) or None, "finished_at_ms": int(f[6]) or None})
    return rows


def node_trust():
    return db("select right(id,8), round(trust_score::numeric,3) from nodes where connected order by created_at").replace("\n", "  ")


# --------------------------------------------------------------------------------------------- main
def main():
    login()
    start = time.time()
    start_ms = int(start * 1000)
    me = call("GET", "/api/portal/customer/me")[1] or {}
    log(f"load test starting; balance {me.get('available_balance_micros', 0) / 1e6:.2f} USD; run {RUN_MINUTES} min + drain {DRAIN_MINUTES} min")
    snapshot("before")
    kinds = {}  # task id -> kind
    cancel_at = {}  # sleeper id -> time to cancel
    submit_errors = {}
    submitted = 0
    cancelled = 0
    health_fail = 0
    peak = {"rss_mb": 0, "load1": 0, "disk_min": 10**9, "in_flight": 0}
    last_probe = 0
    stop_reason = None
    sample_log = []

    def refresh():
        code, tasks = call("GET", "/api/portal/customer/tasks")
        if code != 200 or tasks is None:
            return None
        return [t for t in tasks if t["created_at_ms"] >= start_ms]

    while True:
        elapsed_min = (time.time() - start) / 60
        tasks = refresh()
        if tasks is None:
            health_fail += 1
            log(f"portal not answering (strike {health_fail})")
            if health_fail >= 4 and not stop_reason:
                stop_reason = "coordinator stopped answering"
            time.sleep(TICK)
            if health_fail >= 12:
                break
            continue
        health_fail = 0
        by_state = sql_states(start) or {}
        in_flight = sum(by_state.get(s, 0) for s in IN_FLIGHT)
        peak["in_flight"] = max(peak["in_flight"], in_flight)

        # Stall detector: lots queued and nothing finishing or starting for five minutes = something is wrong
        # (the previous run found a scheduler that silently stopped dispatching). Stop piling on.
        progress = sum(by_state.get(k, 0) for k in ("running", "dispatched", "succeeded", "failed", "cancelled", "abandoned"))
        if progress != getattr(main, "_progress", None):
            main._progress, main._progress_at = progress, time.time()
        if by_state.get("queued", 0) > 30 and time.time() - getattr(main, "_progress_at", time.time()) > 300 and not stop_reason:
            stop_reason = f"STALL: {by_state.get('queued', 0)} queued and no task started or finished for 5 minutes (trust: {node_trust()})"

        # cancel sleepers that have been running a little while
        now = time.time()
        for t in tasks:
            if t["id"] in cancel_at and t["state"] == "running" and now >= cancel_at[t["id"]]:
                code, _ = call("POST", f"/api/portal/customer/tasks/{t['id']}/cancel")
                cancelled += 1 if code == 200 else 0
                del cancel_at[t["id"]]
            elif t["id"] in cancel_at and t["state"] not in IN_FLIGHT:
                del cancel_at[t["id"]]

        if now - last_probe > 120:
            last_probe = now
            p = server_probe()
            peak["rss_mb"] = max(peak["rss_mb"], p.get("rss_mb", 0))
            peak["load1"] = max(peak["load1"], p.get("load1", 0))
            peak["disk_min"] = min(peak["disk_min"], p.get("disk_free_mb", 10**9))
            sample_log.append({"min": round(elapsed_min, 1), **p, "in_flight": in_flight})
            log(f"server: {p} | node trust: {node_trust()}")
            if p.get("active") != "active" and not stop_reason:
                stop_reason = f"coordinator service is {p.get('active')}"
            if p.get("disk_free_mb", 10**9) < 700 and not stop_reason:
                stop_reason = f"server disk low ({p.get('disk_free_mb')} MB free)"
            bal = (call("GET", "/api/portal/customer/me")[1] or {}).get("available_balance_micros", 10**12)
            if bal < 50_000_000 and not stop_reason:
                stop_reason = f"balance low ({bal / 1e6:.2f} USD)"

        ph = phase_at(elapsed_min)
        if ph and not stop_reason:
            _, _, per_tick, cap, weights = ph
            room = max(0, cap - in_flight)
            n = min(per_tick, room)
            for _ in range(n):
                kind = random.choices(list(weights), list(weights.values()))[0]
                code, resp = call("POST", "/api/portal/customer/tasks", mk(kind))
                if code in (200, 201) and resp:
                    kinds[resp["id"]] = kind
                    submitted += 1
                    EVENTS.write(json.dumps({"t": round(time.time() - start, 1), "id": resp["id"], "kind": kind}) + "\n")
                    if kind == "sleeper":
                        cancel_at[resp["id"]] = time.time() + random.randint(25, 70) if random.random() < 0.6 else 0
                        if cancel_at[resp["id"]] == 0:
                            del cancel_at[resp["id"]]
                else:
                    key = f"{code}:{(resp or {}).get('error', '')[:60]}"
                    submit_errors[key] = submit_errors.get(key, 0) + 1
                    if code in (0, 500, 502, 503) and sum(submit_errors.values()) > 30 and not stop_reason:
                        stop_reason = f"too many submit errors: {submit_errors}"
                    if code == 429:
                        time.sleep(2)
                        break

        log(f"min {elapsed_min:5.1f} | submitted {submitted} | in flight {in_flight} | {dict(sorted(by_state.items()))}")

        done_submitting = elapsed_min >= RUN_MINUTES or stop_reason
        if stop_reason and not getattr(main, "_said", False):
            log(f"STOP SUBMITTING: {stop_reason}")
            main._said = True
        if done_submitting and in_flight == 0:
            break
        if done_submitting and elapsed_min >= RUN_MINUTES + DRAIN_MINUTES:
            log("drain time is up; reporting what finished")
            break
        time.sleep(TICK)

    # ------------------------------------------------------------------------------------ report
    final = sql_tasks(start)
    log(f"collected {len(final)} tasks")
    report = {"submitted": submitted, "stop_reason": stop_reason, "submit_errors": submit_errors, "cancel_requests": cancelled,
              "peak": peak, "server_samples": sample_log}
    by_kind = {}
    for t in final:
        k = kinds.get(t["id"], "?")
        d = by_kind.setdefault(k, {"n": 0, "states": {}, "reasons": {}, "queue_s": [], "run_s": []})
        d["n"] += 1
        d["states"][t["state"]] = d["states"].get(t["state"], 0) + 1
        r = t.get("exit_reason") or "-"
        d["reasons"][r] = d["reasons"].get(r, 0) + 1
        if t.get("started_at_ms"):
            d["queue_s"].append((t["started_at_ms"] - t["created_at_ms"]) / 1000)
            if t.get("finished_at_ms"):
                d["run_s"].append((t["finished_at_ms"] - t["started_at_ms"]) / 1000)

    def pct(xs, p):
        if not xs:
            return None
        xs = sorted(xs)
        return round(xs[min(len(xs) - 1, int(len(xs) * p))], 1)

    for k, d in by_kind.items():
        d["queue_p50"], d["queue_p95"], d["queue_max"] = pct(d["queue_s"], 0.5), pct(d["queue_s"], 0.95), pct(d["queue_s"], 1)
        d["run_p50"], d["run_p95"] = pct(d["run_s"], 0.5), pct(d["run_s"], 0.95)
        del d["queue_s"], d["run_s"]
    report["by_kind"] = by_kind
    nodes = {}
    for t in final:
        n = (t.get("node_id") or "none")[-8:]
        nodes[n] = nodes.get(n, 0) + 1
    report["tasks_per_node"] = nodes
    report["states"] = {}
    for t in final:
        report["states"][t["state"]] = report["states"].get(t["state"], 0) + 1
    unexpected = [
        {"id": t["id"], "kind": kinds.get(t["id"], "?"), "state": t["state"], "reason": t.get("exit_reason")}
        for t in final
        if t["state"] in ("failed", "abandoned", "fenced") and kinds.get(t["id"]) not in EXPECTED_FAILURES
        and not (kinds.get(t["id"]) == "sleeper")
    ]
    report["unexpected_failures"] = unexpected[:60]
    report["unexpected_failure_count"] = len(unexpected)
    snapshot("after")
    report["trust_after"] = node_trust()
    report["db_errors"] = db("select count(*) from tasks where created_at > now() - interval '3 hours'")
    report["coordinator_log_warn_error_counts"] = ssh(
        "journalctl -u lazycake-coordinator --since '70 minutes ago' --no-pager -o cat 2>/dev/null | grep -oE 'level=(WARN|ERROR)' | sort | uniq -c | tr '\\n' ' '", 60
    )
    report["coordinator_top_messages"] = ssh(
        "journalctl -u lazycake-coordinator --since '70 minutes ago' --no-pager -o cat 2>/dev/null | grep -E 'level=(WARN|ERROR)' | grep -oE 'msg=\"[^\"]+\"' | sort | uniq -c | sort -rn | head -12", 60
    )
    json.dump(report, open(os.path.join(OUT, "loadtest-report.json"), "w"), indent=2)
    log("REPORT written to loadtest-report.json")
    log(json.dumps({k: v for k, v in report.items() if k not in ("server_samples", "by_kind")}, indent=1))
    for k, d in sorted(by_kind.items()):
        log(f"  {k:10s} n={d['n']:4d} states={d['states']} reasons={d['reasons']} queue p50/p95={d['queue_p50']}/{d['queue_p95']}s run p50={d['run_p50']}s")


if __name__ == "__main__":
    main()
