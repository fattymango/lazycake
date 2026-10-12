# HTTPS and TLS (portal, gRPC, relay)

Back to [README](README.md). Code: `internal/coordinator/tlscert` (`manager.go`, `mux.go`), wiring in `cmd/coordinator/run.go`, config in `internal/coordinator/config`. Tests: `tlscert_test.go`, `TestSessionCookieIsSecureOverTLSEvenInDev`.

## What it does
The coordinator serves **https and plain http on the same port (8080)** and gets its own certificate. No reverse proxy, no certbot, no second port for users.
- **Same port.** `tlscert.Listener` wraps the TCP listener: each connection looks at its first byte on first use (`0x16` = a TLS handshake) and speaks TLS or plain HTTP accordingly. The peek is lazy (in `Read`, with a 10 s timeout), never in `Accept`, so one slow client cannot block others. The connection is not a `*tls.Conn`, so HTTP/1.1 only (fine: SSE works); `tlscert.Wrap` sets `r.TLS` on requests that came in over TLS so `r.TLS != nil` still means https (session cookie `Secure`, `publicBase` for the OAuth/MCP metadata).
- **Certificate.** Let's Encrypt, for the server's **IP address**. IP certificates exist only under the **`shortlived` profile** (about 6.7 days: the default profile answers `rejectedIdentifier: Default profile does not permit IP address identifiers`), so `acme.WithOrderIssuanceProfile("shortlived")` is used for an IP (needs x/crypto ≥ 0.58). A DNS name works too (default profile). Validation is **http-01 on port 80** (tls-alpn-01 would need 443, which another program holds). Renewal at **half life** (`needsRenewal`), checked hourly; the running certificate keeps serving until it expires; the new one is validated (`accept`) before it replaces the files. State in `LAZYCAKE_TLS_DIR` (`account.key`, `cert.pem`, `key.pem`, 0600).
- **Port 80** (`LAZYCAKE_TLS_CHALLENGE_ADDR`, default `:80`) answers only `/.well-known/acme-challenge/<token>`; every other request gets a 308 to `https://host:8080…`. Binding 80 as a non-root user needs `AmbientCapabilities=CAP_NET_BIND_SERVICE`; the firewall must allow 80 (ufw **and** the cloud firewall).
- **`LAZYCAKE_FORCE_HTTPS=1`:** plain-HTTP requests on 8080 get a 308 to https once a certificate is ready; loopback is exempt (the deploy health check `curl http://localhost:8080/` keeps working).

## Config (env)
`LAZYCAKE_TLS_HOST` (IP or DNS name; empty = HTTPS off, plain HTTP as before), `LAZYCAKE_TLS_DIR`, `LAZYCAKE_TLS_ACME_DIRECTORY` (empty = production; staging is `https://acme-staging-v02.api.letsencrypt.org/directory`, trusted by nobody: use it to try the flow), `LAZYCAKE_TLS_CHALLENGE_ADDR`, `LAZYCAKE_FORCE_HTTPS`.

## Production setup (as deployed 2026-10-12, v0.4.25)
Systemd drop-in `/etc/systemd/system/lazycake-coordinator.service.d/https.conf` (kept separate from the unit that holds the DB password): `Environment=LAZYCAKE_TLS_HOST=<ip>`, `LAZYCAKE_TLS_DIR=/opt/lazycake/tls`, `LAZYCAKE_FORCE_HTTPS=1`, and `AmbientCapabilities=CAP_NET_BIND_SERVICE`. ufw allows 80/tcp (comment "lazycake acme http-01 and https redirect"); the droplet's own firewall allows 80 too.

## Gotchas
- **First start has no certificate yet** (issuance takes ~5 s after boot): https handshakes fail for those seconds, plain http works; `FORCE_HTTPS` only redirects once `Ready()`.
- **Short life:** if the coordinator is down for days, or port 80 is unreachable, the certificate expires and https breaks (http keeps working unless FORCE is on and a stale cert is "ready": `Ready` checks expiry). Look for `getting a certificate failed` in the journal; the usual cause is port 80.
- **An IP certificate belongs to the IP.** If the droplet's address changes, change `LAZYCAKE_TLS_HOST` (a loaded certificate for another address is refused).
- Let's Encrypt limits: 5 failed validations per hour per identifier. Try changes against staging first (`/tmp` probe that runs `tlscert.Manager.Run` works; build it from a throwaway `cmd/_probe`).
- Clients validate the certificate for the IP: use `https://<ip>:8080`; names that do not match the certificate fail.

## TLS for agents, gateways and lcctl (v0.4.26)
The same certificate also secures the connections that carry bearer tokens.
- **gRPC (7443)** takes TLS and plaintext on one port (`tlscert.ListenerWith`, same first-byte detection as 8080, ALPN `h2` for gRPC). `LAZYCAKE_REQUIRE_TLS=1` (needs `LAZYCAKE_TLS_HOST`) closes plaintext connections **from other machines** (loopback is allowed: the server's own gateway container uses `127.0.0.1:7443`/`:7444`; `Options.AllowLoopback`) (`ErrPlaintextRefused`; one warning line per 30 s, "update that agent or gateway"). Until it is on, old (≤ v0.4.25) agents and gateways keep working in plaintext.
- **QUIC relay (UDP 7444)** serves the real certificate (`Relay.TLSConfig = tlsMgr.TLSConfig`, ALPN `lazycake-tunnel`, TLS 1.3) instead of a throwaway self-signed one, and clients **verify** it (before, they skipped verification, so a man in the middle could read the token in the control frame). The payload was always Noise-encrypted end to end; this protects the tokens and headers.
- **Clients** (`cmd/agent`, `cmd/gateway`, `cmd/lcctl`, via `internal/transport`): TLS with the verified certificate for every coordinator address that is **not on this machine**, plaintext for loopback (local development, tests). `LAZYCAKE_COORDINATOR_TLS=0` forces plaintext (a dev coordinator on a LAN), `=1` forces TLS. **No automatic fallback to plaintext**: a fallback is a downgrade attack. Containers need `ca-certificates` (the agent and gateway Alpine images have them; the chain Let's Encrypt serves for the IP certificate is trusted by Alpine 3.20).
- **Release guard:** `internal/transport` is part of the shared code hashed for both programs; v0.4.26 bumped `AgentRelease` and `GatewayRelease`.
- **Rollout:** deploy the coordinator first (it accepts both), update every agent and gateway (the dashboard's update command; same machine, token and settings), confirm each reconnected, then set `LAZYCAKE_REQUIRE_TLS=1` in the drop-in and restart. A machine on an old release is refused from then on (it logs reconnect failures; update it).
- **A connection mid-certificate-renewal** is fine: `GetCertificate` serves the current certificate on every handshake. During the ~5 s after a coordinator boot with no certificate yet, TLS handshakes fail and clients retry with backoff.
- **Deadlines:** the first-byte peek restores the deadline the HTTP/gRPC server had set (`lazyConn.SetReadDeadline` remembers it); losing it would have switched off the header and handshake timeouts for the first request (test `TestPeekDoesNotLoosenTheServersDeadlines`).
