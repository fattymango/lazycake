package config

import "testing"

func TestLoadRequiresDatabaseURL(t *testing.T) {
	_, err := Load(func(string) string { return "" })
	if err == nil {
		t.Fatal("expected error for missing LAZYCAKE_DATABASE_URL")
	}
}

func TestLoadDefaults(t *testing.T) {
	env := map[string]string{"LAZYCAKE_DATABASE_URL": "postgres://u:p@host/db"}
	cfg, err := Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.GRPCAddr != ":7443" || cfg.HTTPAddr != ":8080" || cfg.RelayAddr != ":7444" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestRedactURL(t *testing.T) {
	got := redactURL("postgres://user:secret@host:5432/db")
	if got != "postgres://***@host:5432/db" {
		t.Fatalf("got %q", got)
	}
}
