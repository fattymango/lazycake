package config

import "testing"

func TestLoadRequiresFields(t *testing.T) {
	_, err := Load(func(string) string { return "" })
	if err == nil {
		t.Fatal("expected error for missing required fields")
	}
}

func TestParseServices(t *testing.T) {
	svcs, err := parseServices("db:5432, cache:6379")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(svcs) != 2 || svcs[0] != (Service{"db", 5432}) || svcs[1] != (Service{"cache", 6379}) {
		t.Fatalf("unexpected services: %+v", svcs)
	}
}

func TestParseServicesMalformed(t *testing.T) {
	if _, err := parseServices("db"); err == nil {
		t.Fatal("expected error for missing port")
	}
}
