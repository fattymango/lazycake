// Package config loads the agent's configuration from LAZYCAKE_*
// environment variables.
package config

import (
	"fmt"
	"log/slog"
	"net"
	"os"
)

// Config is the agent's full runtime configuration.
type Config struct {
	// CoordinatorAddr is the coordinator's gRPC address, host:port. Required.
	CoordinatorAddr string
	// RelayAddr is the coordinator's QUIC tunnel relay address, host:port.
	// Only needed for tasks that declare tunnel targets; defaults to the
	// same host as CoordinatorAddr on the relay's default port.
	RelayAddr string
	// Token authenticates this agent to the coordinator. Required.
	Token string
	// OfferCores is the number of CPU cores to rent out. Required, > 0.
	OfferCores float64
	// OfferMemoryMB is the amount of RAM to rent out. Required, > 0.
	OfferMemoryMB int
	// OfferDiskMB is the amount of disk to rent out. Required, > 0.
	OfferDiskMB int
	// OfferNetworkMbps is the tunnel bandwidth to rent out, in Mbit/s. Optional: 0 means no limit.
	OfferNetworkMbps int
	// DataDir holds per-task scratch directories and the image cache.
	DataDir string
	// LcinitPath is the host path to the lcinit binary (cmd/lcinit, task
	// 3.6), bind-mounted read-only into every dispatched task's container
	// to enforce wall_timeout_s from the inside. Empty disables the
	// wrapper - if unset, the caller (cmd/agent) looks for a "lcinit"
	// binary next to the agent's own executable before giving up on it.
	LcinitPath string
	// LcinitHostDir is for an agent that itself runs in a container talking
	// to the host's engine: a directory bind-mounted at the *same path* on
	// the host and inside the agent's container. The agent copies its
	// bundled lcinit into it and uses that copy as LcinitPath, because the
	// engine resolves bind-mount sources on the host, where a path inside
	// the agent's own image doesn't exist. Ignored if LcinitPath is set.
	LcinitHostDir string
	// Dev enables human-readable logging instead of JSON.
	Dev bool
}

// DefaultDataDir is where the agent keeps scratch directories and its image cache unless told otherwise.
const DefaultDataDir = "/var/lib/lazycake-agent"

// Load reads configuration from the environment and validates it.
func Load(getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	cfg := Config{
		CoordinatorAddr: getenv("LAZYCAKE_COORDINATOR_ADDR"),
		RelayAddr:       getenv("LAZYCAKE_RELAY_ADDR"),
		Token:           getenv("LAZYCAKE_TOKEN"),
		DataDir:         orDefault(getenv("LAZYCAKE_DATA_DIR"), DefaultDataDir),
		LcinitPath:      getenv("LAZYCAKE_LCINIT_PATH"),
		LcinitHostDir:   getenv("LAZYCAKE_LCINIT_HOST_DIR"),
		Dev:             getenv("LAZYCAKE_DEV") == "1",
	}

	var err error
	if cfg.OfferCores, err = atof(getenv("LAZYCAKE_OFFER_CORES"), 0); err != nil {
		return Config{}, fmt.Errorf("LAZYCAKE_OFFER_CORES: %w", err)
	}
	if cfg.OfferMemoryMB, err = atoi(getenv("LAZYCAKE_OFFER_MEMORY_MB"), 0); err != nil {
		return Config{}, fmt.Errorf("LAZYCAKE_OFFER_MEMORY_MB: %w", err)
	}
	if cfg.OfferDiskMB, err = atoi(getenv("LAZYCAKE_OFFER_DISK_MB"), 0); err != nil {
		return Config{}, fmt.Errorf("LAZYCAKE_OFFER_DISK_MB: %w", err)
	}

	if cfg.OfferNetworkMbps, err = atoi(getenv("LAZYCAKE_OFFER_NETWORK_MBPS"), 0); err != nil {
		return Config{}, fmt.Errorf("LAZYCAKE_OFFER_NETWORK_MBPS: %w", err)
	}
	if cfg.OfferNetworkMbps < 0 {
		return Config{}, fmt.Errorf("LAZYCAKE_OFFER_NETWORK_MBPS must be >= 0")
	}

	if cfg.CoordinatorAddr == "" {
		return Config{}, fmt.Errorf("LAZYCAKE_COORDINATOR_ADDR is required")
	}
	if cfg.Token == "" {
		return Config{}, fmt.Errorf("LAZYCAKE_TOKEN is required")
	}
	if cfg.OfferCores <= 0 {
		return Config{}, fmt.Errorf("LAZYCAKE_OFFER_CORES must be > 0")
	}
	if cfg.OfferMemoryMB <= 0 {
		return Config{}, fmt.Errorf("LAZYCAKE_OFFER_MEMORY_MB must be > 0")
	}
	if cfg.OfferDiskMB <= 0 {
		return Config{}, fmt.Errorf("LAZYCAKE_OFFER_DISK_MB must be > 0")
	}

	if cfg.RelayAddr == "" {
		cfg.RelayAddr = defaultRelayAddr(cfg.CoordinatorAddr)
	}

	return cfg, nil
}

// defaultRelayAddr guesses the relay address from the gRPC coordinator
// address: same host, the relay's default port (see
// internal/coordinator/config's LAZYCAKE_RELAY_ADDR default, ":7444").
func defaultRelayAddr(coordinatorAddr string) string {
	host, _, err := net.SplitHostPort(coordinatorAddr)
	if err != nil {
		return coordinatorAddr
	}
	return net.JoinHostPort(host, "7444")
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func atoi(v string, def int) (int, error) {
	if v == "" {
		return def, nil
	}
	var n int
	_, err := fmt.Sscanf(v, "%d", &n)
	return n, err
}

func atof(v string, def float64) (float64, error) {
	if v == "" {
		return def, nil
	}
	var n float64
	_, err := fmt.Sscanf(v, "%g", &n)
	return n, err
}

// LogValue redacts the auth token so config can be logged safely at startup.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("coordinator_addr", c.CoordinatorAddr),
		slog.String("relay_addr", c.RelayAddr),
		slog.String("token", redact(c.Token)),
		slog.Float64("offer_cores", c.OfferCores),
		slog.Int("offer_memory_mb", c.OfferMemoryMB),
		slog.Int("offer_disk_mb", c.OfferDiskMB),
		slog.Int("offer_network_mbps", c.OfferNetworkMbps),
		slog.String("data_dir", c.DataDir),
		slog.String("lcinit_path", c.LcinitPath),
		slog.String("lcinit_host_dir", c.LcinitHostDir),
		slog.Bool("dev", c.Dev),
	)
}

func redact(s string) string {
	if s == "" {
		return ""
	}
	return "***"
}
