// Package config loads the coordinator's configuration from LAZYCAKE_*
// environment variables.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
)

// Config is the coordinator's full runtime configuration.
type Config struct {
	// DatabaseURL is a postgres:// connection string. Required.
	DatabaseURL string
	// GRPCAddr is the address the agent gRPC service listens on.
	GRPCAddr string
	// HTTPAddr is the address the dashboard and SSE endpoint listen on.
	HTTPAddr string
	// RelayAddr is the UDP address the QUIC tunnel relay listens on.
	RelayAddr string
	// Dev enables human-readable logging instead of JSON.
	Dev bool
}

// Load reads configuration from the environment and validates it.
func Load(getenv func(string) string) (Config, error) {
	if getenv == nil {
		getenv = os.Getenv
	}

	cfg := Config{
		DatabaseURL: getenv("LAZYCAKE_DATABASE_URL"),
		GRPCAddr:    orDefault(getenv("LAZYCAKE_GRPC_ADDR"), ":7443"),
		HTTPAddr:    orDefault(getenv("LAZYCAKE_HTTP_ADDR"), ":8080"),
		RelayAddr:   orDefault(getenv("LAZYCAKE_RELAY_ADDR"), ":7444"),
		Dev:         getenv("LAZYCAKE_DEV") == "1",
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("LAZYCAKE_DATABASE_URL is required")
	}

	return cfg, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// LogValue redacts the database URL's credentials so config can be logged
// safely at startup.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("database_url", redactURL(c.DatabaseURL)),
		slog.String("grpc_addr", c.GRPCAddr),
		slog.String("http_addr", c.HTTPAddr),
		slog.String("relay_addr", c.RelayAddr),
		slog.Bool("dev", c.Dev),
	)
}

func redactURL(u string) string {
	if u == "" {
		return ""
	}
	// postgres://user:pass@host/db -> postgres://***@host/db
	at := -1
	for i, c := range u {
		if c == '@' {
			at = i
			break
		}
	}
	scheme := -1
	for i := 0; i+2 < len(u); i++ {
		if u[i] == ':' && u[i+1] == '/' && u[i+2] == '/' {
			scheme = i + 3
			break
		}
	}
	if at == -1 || scheme == -1 || scheme > at {
		return u
	}
	return u[:scheme] + "***" + u[at:]
}

// MustAtoi parses an integer environment value, returning def on empty or
// invalid input.
func MustAtoi(v string, def int) int {
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
