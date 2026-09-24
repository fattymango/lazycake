// Package runtime abstracts the container engine the agent drives. It is
// written once against the Docker-compatible REST API that both rootless
// Podman and Docker expose, per PLAN.md decision #4 ("a single Runtime
// implementation covers both runtimes"), and against the Runtime interface
// so task dispatch code never depends on which engine is underneath.
package runtime

import (
	"context"
	"io"
	"time"
)

// Spec is everything needed to create one task container. Disk quota is
// deliberately absent: PLAN.md's answer for rootless per-task disk limits
// is a sparse-file-backed scratch mount (internal/agent/imagecache /
// wherever that lands), not a container engine flag, so it is applied by
// the caller via Mounts, not by Runtime itself.
type Spec struct {
	Name       string
	Image      string // must be digest-pinned by the caller
	Entrypoint []string
	Args       []string
	Env        map[string]string
	Workdir    string
	Labels     map[string]string

	// Isolation selects the OCI runtime: "podman" (crun, the default) or
	// "gvisor" (runsc).
	Isolation string

	CPUCores float64
	MemoryMB int
	TmpfsMB  int
	PIDs     int

	// Mounts are bind mounts from host paths to container paths, used for
	// the per-task scratch directory and the lcinit wrapper.
	Mounts []Mount
}

// Mount is one bind mount.
type Mount struct {
	HostPath      string
	ContainerPath string
	ReadOnly      bool
}

// Result is what Wait returns once a container has exited.
type Result struct {
	ExitCode  int
	OOMKilled bool
}

// Runtime is the seam between task dispatch and the container engine.
// PodmanRuntime is the only production implementation; tests depend on
// this interface so they can fake it instead of needing a real engine.
type Runtime interface {
	// Pull fetches image (a digest reference) if not already cached and
	// returns its size in bytes.
	Pull(ctx context.Context, image string) (sizeBytes int64, err error)
	Create(ctx context.Context, spec Spec) (containerID string, err error)
	Start(ctx context.Context, id string) error
	// Wait blocks until the container exits.
	Wait(ctx context.Context, id string) (Result, error)
	Stop(ctx context.Context, id string, grace time.Duration) error
	Logs(ctx context.Context, id string) (io.ReadCloser, error)
	Remove(ctx context.Context, id string) error
	// ListLabelled returns container IDs whose label key=value, for the
	// startup reconciliation sweep (phase 3.7).
	ListLabelled(ctx context.Context, key, value string) ([]string, error)
}
