package netns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/mkassab215/lazycake/internal/agent/runtime"
	"github.com/mkassab215/lazycake/internal/tunnel/noise"
	"github.com/mkassab215/lazycake/internal/tunnel/quic"
)

// SubcommandName is the hidden `agent` subcommand ServeSubcommand handles.
// cmd/agent checks for this as its first argument before normal flag
// parsing, since this process is always started by nsenter, never by a
// human.
const SubcommandName = "__netns_proxy"

// Proxy is the parent-side orchestrator for one task's tunnel. It cannot
// join the container's network namespace itself - setns(CLONE_NEWUSER) is
// documented to fail with EINVAL from a multithreaded caller, and every Go
// process is multithreaded - so it execs nsenter, which does the
// (single-threaded, C, unprivileged-but-same-uid) namespace joins itself
// before exec'ing back into this same agent binary with SubcommandName.
// That child binds every socket (namespaces are inherited across exec, no
// further syscalls needed) and hands the bound file descriptors back over
// a control socket, then exits - a --network=none namespace has no route
// out to the relay at all, so accepting connections and dialing out has
// to happen here, in the parent, which has the host's normal networking.
// The namespace-joining mechanism was proven for real in
// testdata/netns-spike.sh (task 2.1).
type Proxy struct {
	ContainerPID int
	ContainerID  string
	TaskID       string
	Targets      []Target
	AgentKeypair noise.Keypair
	RelayAddr    string
	Token        string
	Runtime      runtime.Runtime
	Log          *slog.Logger

	// EgressCapBytes caps total container->gateway bytes across every
	// connection this task opens; <= 0 means unlimited (PLAN.md task
	// descriptor limits.egress_mb).
	EgressCapBytes int64
	// OnEgressExceeded, if set, fires exactly once when the cap is
	// crossed - after every open connection has already been closed - so
	// the caller can kill the container and report exit_reason
	// "egress_exceeded".
	OnEgressExceeded func()

	// ExecutablePath overrides which binary nsenter execs into for
	// SubcommandName; defaults to os.Executable() (the real agent binary
	// in production). Tests that exercise Proxy directly from a `go test`
	// binary - which does not understand SubcommandName - point this at a
	// freshly built agent binary instead.
	ExecutablePath string

	cmd    *exec.Cmd
	cancel context.CancelFunc
}

// Setup brings up loopback and points resolv.conf at the stub resolver
// (both inside the container's own mount namespace via Runtime.Exec, which
// needs no namespace gymnastics of its own since podman does it), starts
// the bind-only child, receives its bound sockets, dials the relay, and
// starts serving. It returns once serving has started.
func (p *Proxy) Setup(ctx context.Context) error {
	if err := p.Runtime.Exec(ctx, p.ContainerID, []string{"ip", "link", "set", "lo", "up"}); err != nil {
		return fmt.Errorf("bringing up loopback: %w", err)
	}
	if err := p.Runtime.Exec(ctx, p.ContainerID, []string{"sh", "-c", "echo nameserver 127.0.0.53 > /etc/resolv.conf"}); err != nil {
		return fmt.Errorf("writing resolv.conf: %w", err)
	}

	exePath := p.ExecutablePath
	if exePath == "" {
		var err error
		exePath, err = os.Executable()
		if err != nil {
			return fmt.Errorf("resolving own executable path: %w", err)
		}
	}

	cfg := Config{TaskID: p.TaskID, AgentKeypair: p.AgentKeypair, Targets: p.Targets}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshalling proxy config: %w", err)
	}

	parentCtl, childCtl, err := socketpair()
	if err != nil {
		return fmt.Errorf("creating control socketpair: %w", err)
	}
	defer childCtl.Close()

	resultR, resultW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("creating result pipe: %w", err)
	}
	defer resultW.Close()

	nsenterArgs := []string{fmt.Sprintf("--net=/proc/%d/ns/net", p.ContainerPID)}
	if sameUserNamespace(p.ContainerPID) {
		// Sibling-container deployments (this agent itself running in a
		// container talking to the host engine over a shared socket, per
		// executor.go's own doc comment) put every container spawned by
		// the same rootless podman user session in one shared user
		// namespace, agent included - confirmed live by comparing
		// /proc/<pid>/ns/user across the agent's own container and a
		// freshly started sibling. Joining a user namespace you are
		// already a member of fails with EINVAL ("nsenter: setns():
		// can't reassociate to namespace 'user': Invalid argument"), so
		// --user is only added when it actually differs - which is the
		// case for a bare-host-run agent (testdata/netns-spike.sh's
		// setup), where it remains required (--net alone fails EPERM).
	} else {
		nsenterArgs = append(nsenterArgs, fmt.Sprintf("--user=/proc/%d/ns/user", p.ContainerPID))
	}
	nsenterArgs = append(nsenterArgs, "--preserve-credentials", "--", exePath, SubcommandName)
	cmd := exec.CommandContext(ctx, "nsenter", nsenterArgs...)
	cmd.Stdin = bytes.NewReader(cfgJSON)
	childCtlFile, err := childCtl.File()
	if err != nil {
		return fmt.Errorf("getting child control fd: %w", err)
	}
	cmd.ExtraFiles = []*os.File{childCtlFile, resultW}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting netns proxy child: %w", err)
	}
	p.cmd = cmd
	childCtlFile.Close()
	resultW.Close()

	var resultCfg Config
	if err := json.NewDecoder(resultR).Decode(&resultCfg); err != nil {
		cmd.Process.Kill()
		return fmt.Errorf("reading bound config from child (stderr: %s): %w", stderr.String(), err)
	}

	files, err := recvFDs(parentCtl, len(resultCfg.Targets)+1)
	if err != nil {
		cmd.Process.Kill()
		return fmt.Errorf("receiving bound sockets from child (stderr: %s): %w", stderr.String(), err)
	}

	dnsPacketConn, err := net.FilePacketConn(files[0])
	if err != nil {
		return fmt.Errorf("wrapping dns fd: %w", err)
	}
	dnsConn, ok := dnsPacketConn.(*net.UDPConn)
	if !ok {
		return fmt.Errorf("dns fd is not a UDP socket")
	}

	listeners := make([]*net.TCPListener, len(resultCfg.Targets))
	for i, f := range files[1:] {
		lnConn, err := net.FileListener(f)
		if err != nil {
			return fmt.Errorf("wrapping listener fd %d: %w", i, err)
		}
		ln, ok := lnConn.(*net.TCPListener)
		if !ok {
			return fmt.Errorf("listener fd %d is not TCP", i)
		}
		listeners[i] = ln
	}

	relayConn, err := quic.DialAgent(ctx, p.RelayAddr, p.Token)
	if err != nil {
		return fmt.Errorf("dialing relay: %w", err)
	}

	serveCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	serve(serveCtx, resultCfg, dnsConn, listeners, relayConn, p.EgressCapBytes, p.OnEgressExceeded, p.Log)

	go func() {
		if err := cmd.Wait(); err != nil && ctx.Err() == nil {
			p.Log.Warn("netns proxy child exited", "task_id", p.TaskID, "error", err, "stderr", stderr.String())
		}
	}()

	// The child's job (bind + hand off fds) is done well before this; a
	// short pragmatic wait rather than a real readiness signal, matching
	// the same simplification already made for Setup's own return.
	time.Sleep(50 * time.Millisecond)
	return nil
}

// Close stops serving and, if it's still around, the (already-exited,
// normally) child process.
func (p *Proxy) Close() error {
	if p.cancel != nil {
		p.cancel()
	}
	if p.cmd != nil && p.cmd.Process != nil {
		p.cmd.Process.Kill()
	}
	return nil
}

// sameUserNamespace reports whether this process and containerPID are
// already in the same user namespace, by comparing /proc/*/ns/user's
// target inode (the kernel's own identity for a namespace - see
// user_namespaces(7)). A failed read of either side (e.g. this OS lacks
// /proc, or the container's namespace already vanished) is treated as
// "different" - the safe default, matching nsenter's own pre-existing
// behavior of always joining --user.
func sameUserNamespace(containerPID int) bool {
	self, err := os.Readlink("/proc/self/ns/user")
	if err != nil {
		return false
	}
	other, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/user", containerPID))
	if err != nil {
		return false
	}
	return self == other
}

func socketpair() (*net.UnixConn, *net.UnixConn, error) {
	fds, err := socketpairFDs()
	if err != nil {
		return nil, nil, err
	}
	a, err := net.FileConn(os.NewFile(uintptr(fds[0]), "ctl-a"))
	if err != nil {
		return nil, nil, err
	}
	b, err := net.FileConn(os.NewFile(uintptr(fds[1]), "ctl-b"))
	if err != nil {
		return nil, nil, err
	}
	return a.(*net.UnixConn), b.(*net.UnixConn), nil
}
