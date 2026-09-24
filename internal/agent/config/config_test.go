package config

import "testing"

func TestLoadRequiresFields(t *testing.T) {
	_, err := Load(func(string) string { return "" })
	if err == nil {
		t.Fatal("expected error for missing required fields")
	}
}

func TestLoadValid(t *testing.T) {
	env := map[string]string{
		"LAZYCAKE_COORDINATOR_ADDR": "coordinator:7443",
		"LAZYCAKE_TOKEN":            "secret",
		"LAZYCAKE_OFFER_CORES":      "2.5",
		"LAZYCAKE_OFFER_MEMORY_MB":  "4096",
		"LAZYCAKE_OFFER_DISK_MB":    "10240",
	}
	cfg, err := Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.OfferCores != 2.5 || cfg.OfferMemoryMB != 4096 || cfg.OfferDiskMB != 10240 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}
