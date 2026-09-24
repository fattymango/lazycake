//go:build !linux

package capacity

import "fmt"

// Physical is Linux-only (PLAN.md/IMPLEMENTATION.md both scope the agent to
// Linux hosts); this build exists so the module still compiles for
// development on other platforms.
func Physical(dataDir string) (Resources, error) {
	return Resources{}, fmt.Errorf("capacity.Physical: unsupported on this platform, lazycake agents are Linux-only")
}
