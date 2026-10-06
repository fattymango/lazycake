package main

import (
	"os"
	"path/filepath"
	"testing"
)

// stageLcinit looks for lcinit next to the running executable; in a test
// binary that's the temp build dir, so place one there for the test.
func TestStageLcinit(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skip("no executable path")
	}
	bundled := filepath.Join(filepath.Dir(self), "lcinit")
	if err := os.WriteFile(bundled, []byte("v2"), 0o755); err != nil {
		t.Skipf("can't place a fake lcinit next to the test binary: %v", err)
	}
	t.Cleanup(func() { os.Remove(bundled) })

	dir := t.TempDir()
	// A stale copy from an older agent image must be replaced.
	if err := os.WriteFile(filepath.Join(dir, "lcinit"), []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := stageLcinit(dir)
	if err != nil {
		t.Fatalf("stageLcinit: %v", err)
	}
	if got != filepath.Join(dir, "lcinit") {
		t.Fatalf("path = %s", got)
	}
	b, _ := os.ReadFile(got)
	if string(b) != "v2" {
		t.Fatalf("staged content = %q, want v2 (stale copy not replaced)", b)
	}
	if fi, _ := os.Stat(got); fi.Mode().Perm()&0o111 == 0 {
		t.Fatalf("staged lcinit isn't executable: %v", fi.Mode())
	}
	if _, err := os.Stat(got + ".tmp"); err == nil {
		t.Fatalf("temp file left behind")
	}
}
