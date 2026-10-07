//go:build linux

package capacity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFastestLinkIgnoresVirtualInterfacesAndUnreadableSpeeds(t *testing.T) {
	root := t.TempDir()
	mk := func(name string, physical bool, speed string) {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if physical {
			if err := os.MkdirAll(filepath.Join(dir, "device"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if speed != "" {
			if err := os.WriteFile(filepath.Join(dir, "speed"), []byte(speed+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("eth0", true, "1000")
	mk("eth1", true, "2500")
	mk("wlan0", true, "-1")       // wifi: speed unreadable
	mk("veth123", false, "10000") // virtual: must not count
	mk("lo", false, "")
	if got := fastestLinkMbps(root); got != 2500 {
		t.Fatalf("fastest link = %v, want 2500 (the virtual 10000 must be ignored)", got)
	}
	if got := fastestLinkMbps(filepath.Join(root, "missing")); got != 0 {
		t.Fatalf("no interfaces should read as unknown (0), got %v", got)
	}
}
