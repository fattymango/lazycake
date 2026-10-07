//go:build integration

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mkassab215/lazycake/internal/agent/netns"
	lcruntime "github.com/mkassab215/lazycake/internal/agent/runtime"
	"github.com/mkassab215/lazycake/internal/gateway/listener"
	"github.com/mkassab215/lazycake/internal/tunnel/noise"
	"github.com/mkassab215/lazycake/internal/tunnel/quic"
)

type allowAllAuth struct{}

func (allowAllAuth) Authenticate(ctx context.Context, token string) (string, error) {
	return "act_e2e", nil
}

func freeUDPAddr(t *testing.T) string {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{})
	require.NoError(t, err)
	addr := conn.LocalAddr().String()
	conn.Close()
	return addr
}

func startFakePostgres(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go io.Copy(conn, conn)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

// TestTunnelIsolation is task 2.5's verify command made automatic: a
// container with --network=none, running entirely against the real
// stack built in phase 2 (relay, gateway, Noise, the netns proxy itself),
// reaches its one declared target and nothing else - DNS for anything
// unlisted fails outright, and there is no route to anywhere but loopback
// regardless of DNS.
func TestTunnelIsolation(t *testing.T) {
	relayAddr := freeUDPAddr(t)
	log := discardLog()

	relay := &quic.Relay{Auth: allowAllAuth{}, Log: log}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go relay.Serve(ctx, relayAddr)
	time.Sleep(100 * time.Millisecond)

	pgPort := startFakePostgres(t)

	gwKeypair, err := noise.GenerateKeypair()
	require.NoError(t, err)
	gwConn, err := quic.DialGateway(ctx, relayAddr, "gw-token", "gw_1", gwKeypair.Public)
	require.NoError(t, err)
	forwarded := make(chan listener.ForwardStats, 4)
	gw := &listener.Listener{
		Conn: gwConn, Keypair: gwKeypair, Services: map[string]string{"db": fmt.Sprintf("127.0.0.1:%d", pgPort)}, Log: log,
		OnForward: func(s listener.ForwardStats) { forwarded <- s },
	}
	go gw.Run(ctx)

	sock := os.Getenv("LAZYCAKE_TEST_PODMAN_SOCKET")
	if sock == "" {
		sock, err = lcruntime.DefaultSocket()
		require.NoError(t, err)
	}
	rt, err := lcruntime.NewPodmanRuntime(sock)
	require.NoError(t, err)
	defer rt.Close()

	const image = "docker.io/library/alpine:latest"
	_, err = rt.Pull(ctx, image)
	require.NoError(t, err)

	containerID, err := rt.Create(ctx, lcruntime.Spec{
		Name:  "lazycake-tunnel-isolation-test",
		Image: image, Entrypoint: []string{"sleep"}, Args: []string{"300"},
	})
	require.NoError(t, err)
	defer rt.Remove(context.Background(), containerID)
	require.NoError(t, rt.Start(ctx, containerID))

	pid := containerPID(t, sock, containerID)

	agentKeypair, err := noise.GenerateKeypair()
	require.NoError(t, err)

	proxy := &netns.Proxy{
		ContainerPID: pid, ContainerID: containerID, TaskID: "tsk_1",
		Targets:      []netns.Target{{GatewayID: "gw_1", Hostname: "db.acme.com", Port: 5432, NoisePubkey: gwKeypair.Public}},
		AgentKeypair: agentKeypair, RelayAddr: relayAddr, Token: "agent-token",
		Runtime: rt, Log: log, ExecutablePath: buildAgentBinary(t),
	}
	require.NoError(t, proxy.Setup(ctx))
	defer proxy.Close()
	time.Sleep(300 * time.Millisecond) // let the child's listeners come up

	// The container reaches its one declared target: the connection
	// succeeds and real bytes flow all the way through DNS -> TCP ->
	// Noise -> relay -> gateway -> the local service and back, confirmed
	// via the gateway's own byte-count instrumentation rather than
	// busybox nc's stdout (which closes its read side as soon as its
	// piped stdin hits EOF, before necessarily draining the echo back).
	_, err = runInContainer(t, sock, containerID, []string{"sh", "-c", "printf hello | nc -w 3 db.acme.com 5432"})
	require.NoError(t, err)
	select {
	case stats := <-forwarded:
		require.Equal(t, "db", stats.Service)
		require.Greater(t, stats.BytesToLocal, int64(0), "expected the container's bytes to reach the local service")
		// The agent's own tunnel counters (reported in TaskFinished, charted on the task page) agree with what the gateway counted.
		require.Eventually(t, func() bool {
			out, in := proxy.Traffic()
			return out == stats.BytesToLocal && in == stats.BytesToTask
		}, 3*time.Second, 50*time.Millisecond, "agent and gateway should count the same bytes")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the gateway to report a forwarded connection")
	}

	// DNS for anything unlisted fails outright.
	_, err = runInContainer(t, sock, containerID, []string{"timeout", "3", "nslookup", "example.com"})
	require.Error(t, err, "expected DNS resolution for an unlisted host to fail")

	// And there's no route to anywhere but loopback, regardless of DNS -
	// even a raw IP dial must fail since --network=none leaves no
	// interface to route through at all.
	_, err = runInContainer(t, sock, containerID, []string{"timeout", "3", "nc", "-w", "2", "93.184.216.34", "80"})
	require.Error(t, err, "expected a raw external IP to be unreachable")
}

// containerPID and runInContainer shell out to the podman CLI for the two
// things PodmanRuntime doesn't expose (PID lookup, exec-with-output) -
// see runtime.Exec, which only reports pass/fail, not stdout. JSON output
// is parsed directly rather than using `inspect -f`/Go templates: this
// podman-remote build's template engine doesn't resolve .State.Pid (or
// even a plain .Pid) cleanly, while the JSON payload itself has it.
func containerPID(t *testing.T, sock, containerID string) int {
	t.Helper()
	out, err := execPodmanCLI(sock, "inspect", containerID)
	require.NoError(t, err)
	var reports []struct {
		State struct {
			Pid int `json:"Pid"`
		} `json:"State"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &reports))
	require.Len(t, reports, 1)
	return reports[0].State.Pid
}

func runInContainer(t *testing.T, sock, containerID string, cmd []string) (string, error) {
	t.Helper()
	args := append([]string{"exec", containerID}, cmd...)
	return execPodmanCLI(sock, args...)
}

// buildAgentBinary compiles the real agent binary once per test run, since
// Proxy execs into it as a subprocess (see Proxy.ExecutablePath) - a `go
// test` binary doesn't understand the hidden __netns_proxy subcommand.
func buildAgentBinary(t *testing.T) string {
	t.Helper()
	out := t.TempDir() + "/agent"
	cmd := exec.Command("go", "build", "-o", out, "github.com/mkassab215/lazycake/cmd/agent")
	cmd.Dir = repoRoot(t)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "building agent binary: %s", output)
	return out
}

func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").CombinedOutput()
	require.NoError(t, err)
	modFile := string(bytes.TrimSpace(out))
	return modFile[:len(modFile)-len("/go.mod")]
}

func execPodmanCLI(sock string, args ...string) (string, error) {
	fullArgs := append([]string{"--url", "unix://" + sock}, args...)
	out, err := exec.Command("podman", fullArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("podman %v: %w: %s", args, err, out)
	}
	return string(out), nil
}
