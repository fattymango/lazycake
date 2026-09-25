# Multi-stage build producing one small image per LazyCake binary. Build a
# specific target with `--target coordinator|agent|gateway|lcctl`.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/coordinator ./cmd/coordinator \
 && go build -o /out/agent ./cmd/agent \
 && go build -o /out/gateway ./cmd/gateway \
 && go build -o /out/lcctl ./cmd/lcctl \
 && CGO_ENABLED=0 go build -o /out/lcinit ./cmd/lcinit \
 && CGO_ENABLED=0 go build -o /out/refworkload ./cmd/refworkload

FROM alpine:3.20 AS coordinator
RUN apk add --no-cache ca-certificates
COPY --from=build /out/coordinator /usr/local/bin/coordinator
ENTRYPOINT ["/usr/local/bin/coordinator"]

FROM alpine:3.20 AS agent
# podman-remote gives the agent's own capability probe a `podman` CLI to
# shell out to (internal/agent/probe), talking to the host's engine over
# the socket mounted at $XDG_RUNTIME_DIR/podman/podman.sock - the agent's
# actual container runtime uses that same socket directly via the Docker
# API and never needs the CLI, but the probe's checks are written against
# podman-the-command since that's the honest, portable way to exercise
# --memory/--cpus/--pids-limit enforcement.
RUN apk add --no-cache ca-certificates podman-remote shadow \
 && ln -s /usr/bin/podman-remote /usr/bin/podman \
 && usermod --add-subuids 65536-131071 --add-subgids 65536-131071 root
COPY --from=build /out/agent /usr/local/bin/agent
# lcinit lives next to the agent binary so cmd/agent's discoverLcinit finds
# it with zero configuration (task 3.6) - it's bind-mounted read-only into
# every dispatched task's own container, so it has to be a fully static
# binary (CGO_ENABLED=0 above) rather than dynamically linked against this
# image's musl, which an arbitrary customer image won't have.
COPY --from=build /out/lcinit /usr/local/bin/lcinit
ENTRYPOINT ["/usr/local/bin/agent"]

FROM alpine:3.20 AS gateway
RUN apk add --no-cache ca-certificates
COPY --from=build /out/gateway /usr/local/bin/gateway
ENTRYPOINT ["/usr/local/bin/gateway"]

FROM alpine:3.20 AS lcctl
RUN apk add --no-cache ca-certificates
COPY --from=build /out/lcctl /usr/local/bin/lcctl
ENTRYPOINT ["/usr/local/bin/lcctl"]

# refworkload (task 6.3's reference workload, Monte Carlo option pricing)
# is what actually gets dispatched to agents, so its image needs to be
# small and digest-pinnable on its own, not bundled with anything else.
FROM alpine:3.20 AS refworkload
COPY --from=build /out/refworkload /usr/local/bin/refworkload
ENTRYPOINT ["/usr/local/bin/refworkload"]
