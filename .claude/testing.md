# Testing

Back to [README](README.md). Owner's expectation: production quality, verified for real; tests that can fail; honest reporting. Commands also in `CLAUDE.md` §6.

## Layers
1. **Unit tests** (no tag): `go test ./...`. Fakes everywhere via interfaces (`fakeStore`, fake gRPC streams in `conn`, `clock.Fake`).
2. **Integration** (`//go:build integration`): real Postgres (`LAZYCAKE_TEST_DATABASE_URL`) and/or real rootless podman (`LAZYCAKE_TEST_PODMAN_SOCKET`, default socket auto-detected). `go test -race -tags=integration -p 1 ./...` (`-p 1` because packages share the database and podman).
3. **`internal/e2e`** (integration only): coordinator + agent in-process: dispatch, stop, trust ban, tunnel isolation. **All its files must carry the `integration` tag**, or plain `go vet ./...` fails to compile the package. `go list ./...` without the tag omits it, so list with `-tags=integration`.
4. **Self-fence** (`internal/agent/lease/selffence_integration_test.go`): real container, a blockable fake link; asserts the container dies on its own while cut off **and** that the fenced final report is delivered after reconnect (a wrapper observes the report finding no connection, so the replay path is really exercised).
5. **Frontend:** `make web-check` (tsc/vite build, eslint, prettier, vitest) and **browser scripts** `web/e2e/*.mjs` against a coordinator serving the production build.
6. **Load test:** `tools/loadtest` against production only with the owner's go-ahead.

## Full regression (before a release)
```
go vet ./... && go vet -tags=integration ./...
go test -race -tags=integration -p 1 -count=1 $(go list -tags=integration ./... | grep -v internal/agent/bench)
make web-check
# browser: behaviour, addmachine, update (+ traffic/usage/taskusage when live data exists)
```
Runtime ≈ 10 minutes. `internal/agent/bench` `TestBenchStability` fails on a busy desktop or with `-race` (timing), excluded.

## Habits that mattered
- **Mutation check:** after adding a regression test, temporarily revert the fix and confirm the test fails (done for the runner ordering bug, the version cap, the hold release).
- A test that waits for something must first prove the precondition (assert the container is running before expecting it to stop; a wrong container name once made a wait pass instantly).
- Go glob trap: `git log --branches=master` matches `refs/heads/master/*`, not `master`.
- When a fake store lacks a new method the whole `api` test package fails to compile — update `api/fakestore_test.go`.
- Browser scripts: `[role="status"]` can match alerts; use the `data-chart-tip` marker for chart tooltips. Prettier must be run on touched web files.
- Local demo coordinator for browser checks: see `CLAUDE.md` §4; kill and restart it after rebuilding web assets.
