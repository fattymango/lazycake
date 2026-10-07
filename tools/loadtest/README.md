# Load test

Submits an hour of mixed work through the customer portal API and reports what happened: many task sizes, CPU and memory
benchmarks, network tasks through two gateways (tiny requests and 50 MB downloads), tasks that must fail (out of memory,
timeout, non-zero exit, nonexistent image), tasks that are cancelled while running, and a burst of up to 300 in flight.
Safety stops: the coordinator stops answering, the server's free disk falls below 700 MB, the balance falls below $50,
the in-flight cap is reached, or the queue stalls for 5 minutes (nothing starts or finishes).

```
export LAZYCAKE_BASE=http://coordinator:8080 LAZYCAKE_USER=you LAZYCAKE_PASSWORD=...
export LAZYCAKE_GW_SMALL=gw_...   # a gateway publishing "testfile":8000 (a tiny /test.txt)
export LAZYCAKE_GW_BIG=gw_...     # a gateway publishing "files":8000 with a 50 MB /loadtest-50MB.bin
export LAZYCAKE_SSH=user@coordinator-host   # optional: health probes and database counts over ssh (podman postgres)
python3 tools/loadtest/loadtest.py
```

Needs at least one connected machine. Results land in `loadtest.log`, `loadtest-events.jsonl` and `loadtest-report.json`
(`LAZYCAKE_LOADTEST_OUT` chooses the directory). See `docs/LOAD_TEST_2026-10-07.md` for what the first runs found.
