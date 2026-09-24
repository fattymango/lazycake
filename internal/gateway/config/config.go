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
// Host defaults to 127.0.0.1 (the common case: the gateway runs on the
// same machine as the service, which is what proves control of it -
// PLAN.md overview); an explicit host is only needed when that's not
// true, e.g. this project's own docker-compose demo, where the gateway
// and its "customer database" are necessarily separate containers.
type Service struct {
	Name string
	Host string
	Port int
}

// Addr is the host:port to dial for this service.
func (s Service) Addr() string {
	return fmt.Sprintf("%s:%d", s.Host, s.Port)
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

// parseServices accepts "name:port" (host defaults to 127.0.0.1) or
// "name:host:port", comma-separated.
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
		fields := strings.Split(part, ":")
		var svc Service
		switch len(fields) {
		case 2:
			svc = Service{Name: fields[0], Host: "127.0.0.1"}
			if _, err := fmt.Sscanf(fields[1], "%d", &svc.Port); err != nil || svc.Port <= 0 {
				return nil, fmt.Errorf("malformed port in %q", part)
			}
		case 3:
			svc = Service{Name: fields[0], Host: fields[1]}
			if _, err := fmt.Sscanf(fields[2], "%d", &svc.Port); err != nil || svc.Port <= 0 {
				return nil, fmt.Errorf("malformed port in %q", part)
			}
		default:
			return nil, fmt.Errorf("malformed entry %q, want name:port or name:host:port", part)
		}
		out = append(out, svc)
	}
	return out, nil
}

// LogValue redacts the auth token so config can be logged safely at startup.
func (c Config) LogValue() slog.Value {
	names := make([]string, len(c.Services))
	for i, s := range c.Services {
		names[i] = fmt.Sprintf("%s:%s", s.Name, s.Addr())
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
