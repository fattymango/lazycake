package runtime

import (
	"fmt"
	"os"
)

// DefaultSocket returns the first reachable engine socket: the rootless
// Podman API socket under $XDG_RUNTIME_DIR, then the Docker socket, per
// IMPLEMENTATION.md task 1.6.
func DefaultSocket() (string, error) {
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		p := xdg + "/podman/podman.sock"
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	if _, err := os.Stat("/var/run/docker.sock"); err == nil {
		return "/var/run/docker.sock", nil
	}
	return "", fmt.Errorf("no container engine socket found (checked $XDG_RUNTIME_DIR/podman/podman.sock and /var/run/docker.sock)")
}
