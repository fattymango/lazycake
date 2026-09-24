// Package config loads the agent's configuration from LAZYCAKE_*
// environment variables.
package config

import (
	"fmt"
	"log/slog"
	"os"
)

// Config is the agent's full runtime configuration.
type Config struct {
	// CoordinatorAddr is the coordinator's gRPC address, host:port. Required.
	CoordinatorAddr string
	// Token authenticates this agent to the coordinator. Required.
	Token string
	// OfferCores is the number of CPU cores to rent out. Required, > 0.
	OfferCores float64
	// OfferMemoryMB is the amount of RAM to rent out. Required, > 0.
	OfferMemoryMB int
	// OfferDiskMB is the amount of disk to rent out. Required, > 0.
	OfferDiskMB int
	// DataDir holds per-task scratch directories and the image cache.
	DataDir string
	// Dev enables human-readable logging instead of JSON.
	Dev bool
}

// Load reads configuration from the environment and validates it.
func Load(getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	cfg := Config{
		CoordinatorAddr: getenv("LAZYCAKE_COORDINATOR_ADDR"),
		Token:           getenv("LAZYCAKE_TOKEN"),
		DataDir:         orDefault(getenv("LAZYCAKE_DATA_DIR"), "/var/lib/lazycake-agent"),
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

	return cfg, nil
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
		slog.String("token", redact(c.Token)),
		slog.Float64("offer_cores", c.OfferCores),
		slog.Int("offer_memory_mb", c.OfferMemoryMB),
		slog.Int("offer_disk_mb", c.OfferDiskMB),
		slog.String("data_dir", c.DataDir),
		slog.Bool("dev", c.Dev),
	)
}

func redact(s string) string {
	if s == "" {
		return ""
	}
	return "***"
}
