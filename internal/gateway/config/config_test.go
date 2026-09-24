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
	want := []Service{{Name: "db", Host: "127.0.0.1", Port: 5432}, {Name: "cache", Host: "127.0.0.1", Port: 6379}}
	if len(svcs) != 2 || svcs[0] != want[0] || svcs[1] != want[1] {
		t.Fatalf("unexpected services: %+v", svcs)
	}
}

func TestParseServicesExplicitHost(t *testing.T) {
	svcs, err := parseServices("db:customer-db:5432")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := Service{Name: "db", Host: "customer-db", Port: 5432}
	if len(svcs) != 1 || svcs[0] != want {
		t.Fatalf("unexpected services: %+v", svcs)
	}
}

func TestParseServicesMalformed(t *testing.T) {
	if _, err := parseServices("db"); err == nil {
		t.Fatal("expected error for missing port")
	}
}
