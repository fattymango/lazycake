package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// The id must survive the agent being stopped and started again (a new process, a recreated container with the same
// mounted directory), and two machines must not share one.
func TestMachineIDIsStableAcrossRestartsAndDistinctPerMachine(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	first := loadOrCreateMachineID(a, quiet())
	if first == "" {
		t.Fatal("no id created")
	}
	if again := loadOrCreateMachineID(a, quiet()); again != first {
		t.Fatalf("the id changed across a restart: %q then %q", first, again)
	}
	if other := loadOrCreateMachineID(b, quiet()); other == first || other == "" {
		t.Fatalf("a different machine got %q (same as %q or empty)", other, first)
	}
	if _, err := os.Stat(filepath.Join(a, machineIDFile+".tmp")); err == nil {
		t.Fatal("left the temporary file behind")
	}
}

// An unwritable location degrades to "no machine id" (the old per-process identity), never to a crash or a wrong id.
func TestMachineIDUnwritableDirGivesNone(t *testing.T) {
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadOrCreateMachineID(filepath.Join(f, "sub"), quiet()); got != "" {
		t.Fatalf("got %q for an unwritable directory", got)
	}
}
