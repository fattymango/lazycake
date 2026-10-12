package version

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// What the agent and the gateway are built from (paths relative to the repo root): their commands and internals, the transport pieces
// both share with the coordinator's relay (not the relay itself), and the generated messages each speaks.
var sharedCode = []string{
	"internal/id", "internal/logging", "internal/transport", "internal/tunnel/noise",
	"internal/tunnel/quic/client.go", "internal/tunnel/quic/frame.go", "internal/tunnel/quic/probe.go", "internal/tunnel/quic/tls.go",
}

var (
	agentCode = append([]string{
		"cmd/agent", "internal/agent",
		"internal/proto/lazycake/v1/agent.pb.go", "internal/proto/lazycake/v1/agent_grpc.pb.go",
	}, sharedCode...)
	gatewayCode = append([]string{
		"cmd/gateway", "internal/gateway",
		"internal/proto/lazycake/v1/gateway.pb.go", "internal/proto/lazycake/v1/gateway_grpc.pb.go",
	}, sharedCode...)
)

// The release in which each program's code (hashed below) was last changed, and its hash. Change a pair together with its release constant.
const (
	recordedAgentHash      = "8d86cdc1cc94787c648217bfe1a72803ae87c83bd2599fcdebb503d2789d1f2f"
	recordedAgentRelease   = "v0.4.26"
	recordedGatewayHash    = "8db6986dc66cf7c1a36568fd0d5685b8af95301b898b935e629dccdc983f15bf"
	recordedGatewayRelease = "v0.4.26"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the source tree")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// codeHash hashes every non-test Go file under the listed paths, by relative path and content, in a stable order.
func codeHash(t *testing.T, root string, paths []string) string {
	t.Helper()
	var files []string
	for _, p := range paths {
		full := filepath.Join(root, p)
		info, err := os.Stat(full)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if !info.IsDir() {
			files = append(files, p)
			continue
		}
		err = filepath.WalkDir(full, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
				rel, _ := filepath.Rel(root, path)
				files = append(files, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatal(err)
		}
		h.Write([]byte(f))
		h.Write([]byte{0})
		h.Write(b)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func checkProgram(t *testing.T, name string, paths []string, recordedHash, recordedRelease, release string) {
	t.Helper()
	got := codeHash(t, repoRoot(t), paths)
	if got != recordedHash {
		t.Fatalf("the %s code changed since release %s.\n"+
			"Set the %s release constant in version.go to the release you are cutting, then set its recorded hash to %q and its recorded release to that release in this file.\n"+
			"(Re-record the hash even if the change was only a comment: it is the release number that tells owners to update.)",
			name, recordedRelease, name, got)
	}
	if recordedRelease != release {
		t.Fatalf("the recorded %s release (%s) and the constant in version.go (%s) must be changed together", name, recordedRelease, release)
	}
}

// An agent or gateway edit that is not announced would leave every running one looking "current" while it lacks the change: these fail
// until the release number is bumped (and the hash re-recorded), so the dashboard can tell owners to update.
func TestAgentReleaseIsBumpedWithItsCode(t *testing.T) {
	checkProgram(t, "agent", agentCode, recordedAgentHash, recordedAgentRelease, AgentRelease)
}

func TestGatewayReleaseIsBumpedWithItsCode(t *testing.T) {
	checkProgram(t, "gateway", gatewayCode, recordedGatewayHash, recordedGatewayRelease, GatewayRelease)
}
