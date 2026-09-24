// Package config loads the gateway's configuration from LAZYCAKE_*
// environment variables.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// Service is one local TCP service the gateway is willing to forward to.
type Service struct {
	Name string
	Port int
}

// Config is the gateway's full runtime configuration.
type Config struct {
	// CoordinatorAddr is the coordinator's relay address, host:port. Required.
	CoordinatorAddr string
	// Token authenticates this gateway to the coordinator. Required.
	Token string
	// GatewayID is the gw_<ulid> `lcctl gateway create` printed alongside
	// this token. Required.
	GatewayID string
	// Services are the local services this gateway will forward to, e.g.
	// "db:5432,cache:6379". Required, at least one.
	Services []Service
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
		GatewayID:       getenv("LAZYCAKE_GATEWAY_ID"),
		Dev:             getenv("LAZYCAKE_DEV") == "1",
	}

	svcs, err := parseServices(getenv("LAZYCAKE_SERVICES"))
	if err != nil {
		return Config{}, fmt.Errorf("LAZYCAKE_SERVICES: %w", err)
	}
	cfg.Services = svcs

	if cfg.CoordinatorAddr == "" {
		return Config{}, fmt.Errorf("LAZYCAKE_COORDINATOR_ADDR is required")
	}
	if cfg.Token == "" {
		return Config{}, fmt.Errorf("LAZYCAKE_TOKEN is required")
	}
	if cfg.GatewayID == "" {
		return Config{}, fmt.Errorf("LAZYCAKE_GATEWAY_ID is required")
	}
	if len(cfg.Services) == 0 {
		return Config{}, fmt.Errorf("LAZYCAKE_SERVICES is required, e.g. \"db:5432\"")
	}

	return cfg, nil
}

func parseServices(v string) ([]Service, error) {
	if v == "" {
		return nil, nil
	}
	var out []Service
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, portStr, ok := strings.Cut(part, ":")
		if !ok {
			return nil, fmt.Errorf("malformed entry %q, want name:port", part)
		}
		var port int
		if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil || port <= 0 {
			return nil, fmt.Errorf("malformed port in %q", part)
		}
		out = append(out, Service{Name: name, Port: port})
	}
	return out, nil
}

// LogValue redacts the auth token so config can be logged safely at startup.
func (c Config) LogValue() slog.Value {
	names := make([]string, len(c.Services))
	for i, s := range c.Services {
		names[i] = fmt.Sprintf("%s:%d", s.Name, s.Port)
	}
	return slog.GroupValue(
		slog.String("coordinator_addr", c.CoordinatorAddr),
		slog.String("token", redact(c.Token)),
		slog.String("gateway_id", c.GatewayID),
		slog.String("services", strings.Join(names, ",")),
		slog.Bool("dev", c.Dev),
	)
}

func redact(s string) string {
	if s == "" {
		return ""
	}
	return "***"
}
