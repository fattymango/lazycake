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

// The release in which the code hashed below was last changed, and its hash. Change these two together with AgentGatewayRelease.
const (
	recordedCodeHash   = "82e69f303a0b4a2c5cf8ea4fca0b026e3ddd48613c5cc09020d96d8f2576e380"
	recordedForRelease = "v0.4.4"
)

// agentGatewayCode lists what makes up the agent and gateway programs (relative to the repo root): their commands and internals,
// the transport pieces they share with the coordinator's relay (not the relay itself), and the generated messages they speak.
var agentGatewayCode = []string{
	"cmd/agent", "cmd/gateway", "internal/agent", "internal/gateway", "internal/id", "internal/logging",
	"internal/tunnel/noise",
	"internal/tunnel/quic/client.go", "internal/tunnel/quic/frame.go", "internal/tunnel/quic/probe.go", "internal/tunnel/quic/tls.go",
	"internal/proto/lazycake/v1/agent.pb.go", "internal/proto/lazycake/v1/agent_grpc.pb.go",
	"internal/proto/lazycake/v1/gateway.pb.go", "internal/proto/lazycake/v1/gateway_grpc.pb.go",
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the source tree")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// codeHash hashes every non-test Go file under the listed paths, by relative path and content, in a stable order.
func codeHash(t *testing.T, root string) string {
	t.Helper()
	var files []string
	for _, p := range agentGatewayCode {
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

// An agent or gateway edit that is not announced would leave every running one looking "current" while it lacks the change: this
// fails until the release number is bumped (and the hash re-recorded), so the dashboard can tell owners to update.
func TestAgentGatewayReleaseIsBumpedWithTheirCode(t *testing.T) {
	got := codeHash(t, repoRoot(t))
	if got != recordedCodeHash {
		t.Fatalf("the agent or gateway code changed since release %s.\n"+
			"Set AgentGatewayRelease in version.go to the release you are cutting, then set recordedCodeHash to %q and recordedForRelease to that release in this file.\n"+
			"(Only do that if the change really matters to running agents and gateways; if it was a comment or a test helper, still re-record the hash.)",
			recordedForRelease, got)
	}
	if recordedForRelease != AgentGatewayRelease {
		t.Fatalf("recordedForRelease (%s) and AgentGatewayRelease (%s) must be changed together", recordedForRelease, AgentGatewayRelease)
	}
}
