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

func TestOfferNetworkMbpsIsOptionalAndNeverNegative(t *testing.T) {
	base := map[string]string{
		"LAZYCAKE_COORDINATOR_ADDR": "c:7443", "LAZYCAKE_TOKEN": "t",
		"LAZYCAKE_OFFER_CORES": "2", "LAZYCAKE_OFFER_MEMORY_MB": "1024", "LAZYCAKE_OFFER_DISK_MB": "2048",
	}
	get := func(extra map[string]string) func(string) string {
		return func(k string) string {
			if v, ok := extra[k]; ok {
				return v
			}
			return base[k]
		}
	}
	cfg, err := Load(get(nil))
	if err != nil || cfg.OfferNetworkMbps != 0 {
		t.Fatalf("unset must mean no limit: %+v %v", cfg, err)
	}
	cfg, err = Load(get(map[string]string{"LAZYCAKE_OFFER_NETWORK_MBPS": "250"}))
	if err != nil || cfg.OfferNetworkMbps != 250 {
		t.Fatalf("got %+v %v", cfg, err)
	}
	if _, err := Load(get(map[string]string{"LAZYCAKE_OFFER_NETWORK_MBPS": "-5"})); err == nil {
		t.Fatal("a negative network offer must be rejected")
	}
}
