package runtime

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	dockerclient "github.com/docker/docker/client"
)

// PodmanRuntime implements Runtime against any Docker-compatible REST API -
// rootless Podman's socket in production, Docker's in local dev if that's
// what's available (PLAN.md decision #4: one implementation, one API).
type PodmanRuntime struct {
	cli *dockerclient.Client
}

// NewPodmanRuntime connects to the engine listening on socketPath.
func NewPodmanRuntime(socketPath string) (*PodmanRuntime, error) {
	cli, err := dockerclient.NewClientWithOpts(
		dockerclient.WithHost("unix://"+socketPath),
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", socketPath, err)
	}
	return &PodmanRuntime{cli: cli}, nil
}

// Close releases the underlying HTTP client's connections.
func (r *PodmanRuntime) Close() error { return r.cli.Close() }

var _ Runtime = (*PodmanRuntime)(nil)

func (r *PodmanRuntime) Pull(ctx context.Context, image string) (int64, error) {
	rc, err := r.cli.ImagePull(ctx, image, types.ImagePullOptions{})
	if err != nil {
		return 0, fmt.Errorf("pulling %s: %w", image, err)
	}
	// Drain the pull progress stream; ImagePull's body must be read to
	// completion for the pull to actually finish.
	if _, err := io.Copy(io.Discard, rc); err != nil {
		rc.Close()
		return 0, fmt.Errorf("reading pull progress for %s: %w", image, err)
	}
	rc.Close()

	inspect, _, err := r.cli.ImageInspectWithRaw(ctx, image)
	if err != nil {
		return 0, fmt.Errorf("inspecting %s after pull: %w", image, err)
	}
	return inspect.Size, nil
}

func (r *PodmanRuntime) Create(ctx context.Context, spec Spec) (string, error) {
	cfg := &container.Config{
		Image:      spec.Image,
		Entrypoint: spec.Entrypoint,
		Cmd:        spec.Args,
		Env:        envList(spec.Env),
		WorkingDir: spec.Workdir,
		Labels:     spec.Labels,
		// Tty merges stdout/stderr into one unframed stream. Without it the
		// engine multiplexes both streams with an 8-byte header per frame
		// (the stdcopy format), which Logs callers would otherwise have to
		// demultiplex; tasks never get an interactive terminal regardless.
		Tty: true,
	}

	hostCfg := &container.HostConfig{
		NetworkMode: "none",
		Resources: container.Resources{
			NanoCPUs:  int64(spec.CPUCores * 1e9),
			Memory:    int64(spec.MemoryMB) << 20,
			PidsLimit: pidsLimitPtr(spec.PIDs),
		},
		// Pinned rather than inherited: a host defaulting to the journald
		// log driver (podman's own default on some distros) makes the
		// Docker-API ContainerLogs call Logs() relies on come back empty -
		// k8s-file is what that API path actually reads from reliably.
		LogConfig: container.LogConfig{Type: "k8s-file"},
	}
	if spec.Isolation == "gvisor" {
		hostCfg.Runtime = "runsc"
	}
	if spec.TmpfsMB > 0 {
		hostCfg.Tmpfs = map[string]string{"/tmp": fmt.Sprintf("size=%dm", spec.TmpfsMB)}
	}
	for _, m := range spec.Mounts {
		hostCfg.Mounts = append(hostCfg.Mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   m.HostPath,
			Target:   m.ContainerPath,
			ReadOnly: m.ReadOnly,
		})
	}

	resp, err := r.cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, spec.Name)
	if err != nil {
		return "", fmt.Errorf("creating container: %w", err)
	}
	return resp.ID, nil
}

func (r *PodmanRuntime) Start(ctx context.Context, id string) error {
	if err := r.cli.ContainerStart(ctx, id, types.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("starting container %s: %w", id, err)
	}
	return nil
}

func (r *PodmanRuntime) Wait(ctx context.Context, id string) (Result, error) {
	statusCh, errCh := r.cli.ContainerWait(ctx, id, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		return Result{}, fmt.Errorf("waiting for container %s: %w", id, err)
	case status := <-statusCh:
		inspect, err := r.cli.ContainerInspect(ctx, id)
		if err != nil {
			return Result{}, fmt.Errorf("inspecting container %s after wait: %w", id, err)
		}
		return Result{
			ExitCode:  int(status.StatusCode),
			OOMKilled: inspect.State != nil && inspect.State.OOMKilled,
		}, nil
	}
}

func (r *PodmanRuntime) Stop(ctx context.Context, id string, grace time.Duration) error {
	secs := int(grace.Seconds())
	if err := r.cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &secs}); err != nil {
		return fmt.Errorf("stopping container %s: %w", id, err)
	}
	return nil
}

func (r *PodmanRuntime) Logs(ctx context.Context, id string) (io.ReadCloser, error) {
	rc, err := r.cli.ContainerLogs(ctx, id, types.ContainerLogsOptions{
		ShowStdout: true, ShowStderr: true, Follow: true,
	})
	if err != nil {
		return nil, fmt.Errorf("streaming logs for %s: %w", id, err)
	}
	return rc, nil
}

func (r *PodmanRuntime) Remove(ctx context.Context, id string) error {
	if err := r.cli.ContainerRemove(ctx, id, types.ContainerRemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("removing container %s: %w", id, err)
	}
	return nil
}

func (r *PodmanRuntime) Pid(ctx context.Context, containerID string) (int, error) {
	inspect, err := r.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return 0, fmt.Errorf("inspecting container %s: %w", containerID, err)
	}
	if inspect.State == nil {
		return 0, fmt.Errorf("container %s has no state", containerID)
	}
	return inspect.State.Pid, nil
}

func (r *PodmanRuntime) Exec(ctx context.Context, containerID string, cmd []string) error {
	resp, err := r.cli.ContainerExecCreate(ctx, containerID, types.ExecConfig{
		Cmd: cmd, AttachStdout: true, AttachStderr: true,
	})
	if err != nil {
		return fmt.Errorf("creating exec for %s: %w", containerID, err)
	}

	attach, err := r.cli.ContainerExecAttach(ctx, resp.ID, types.ExecStartCheck{})
	if err != nil {
		return fmt.Errorf("attaching exec for %s: %w", containerID, err)
	}
	defer attach.Close()
	output, _ := io.ReadAll(attach.Reader)

	inspect, err := r.cli.ContainerExecInspect(ctx, resp.ID)
	if err != nil {
		return fmt.Errorf("inspecting exec for %s: %w", containerID, err)
	}
	if inspect.ExitCode != 0 {
		return fmt.Errorf("exec %v in %s exited %d: %s", cmd, containerID, inspect.ExitCode, output)
	}
	return nil
}

func (r *PodmanRuntime) ListLabelled(ctx context.Context, key, value string) ([]string, error) {
	f := filters.NewArgs(filters.Arg("label", fmt.Sprintf("%s=%s", key, value)))
	containers, err := r.cli.ContainerList(ctx, types.ContainerListOptions{All: true, Filters: f})
	if err != nil {
		return nil, fmt.Errorf("listing containers labelled %s=%s: %w", key, value, err)
	}
	ids := make([]string, len(containers))
	for i, c := range containers {
		ids[i] = c.ID
	}
	return ids, nil
}

func (r *PodmanRuntime) LabelledContainers(ctx context.Context, key string) ([]LabelledContainer, error) {
	f := filters.NewArgs(filters.Arg("label", key))
	containers, err := r.cli.ContainerList(ctx, types.ContainerListOptions{All: true, Filters: f})
	if err != nil {
		return nil, fmt.Errorf("listing containers labelled %s: %w", key, err)
	}
	out := make([]LabelledContainer, len(containers))
	for i, c := range containers {
		out[i] = LabelledContainer{ID: c.ID, Labels: c.Labels}
	}
	return out, nil
}

func envList(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

func pidsLimitPtr(n int) *int64 {
	if n <= 0 {
		return nil
	}
	v := int64(n)
	return &v
}
