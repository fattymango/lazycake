package portalapi

import (
	"strings"
	"testing"
)

// The install command is copy-pasted by providers, so the things that
// have each broken a real install must stay in it.
func TestInstallCommand(t *testing.T) {
	cmd := (&Server{CoordinatorAddr: "coord.example:7443"}).installCommand("tok123")
	for _, want := range []string{
		"docker.io/fattymango/lazycake-agent:latest", // a bare name resolves against docker.io/library and is denied
		"--pid=host --cap-add=SYS_ADMIN",             // tunnel tasks nsenter into sibling containers
		"-e LAZYCAKE_TOKEN=tok123",
		"-e LAZYCAKE_COORDINATOR_ADDR=coord.example:7443",
		"mkdir -p $HOME/.local/share/lazycake/bin",                           // -v fails if the host dir is missing
		"-v $HOME/.local/share/lazycake/bin:$HOME/.local/share/lazycake/bin", // same path on both sides
		"-e LAZYCAKE_LCINIT_HOST_DIR=$HOME/.local/share/lazycake/bin",        // so the agent stages lcinit there
		"/run/user/$(id -u)/podman/podman.sock",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("install command is missing %q:\n%s", want, cmd)
		}
	}
}
