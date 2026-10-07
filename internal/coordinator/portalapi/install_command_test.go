package portalapi

import (
	"math"
	"strings"
	"testing"
)

// The install command is copy-pasted by providers, so the things that
// have each broken a real install must stay in it.
func TestInstallCommand(t *testing.T) {
	cmd := (&Server{CoordinatorAddr: "coord.example:7443"}).installCommand("tok123", defaultMachineOffer())
	for _, want := range []string{
		"docker.io/fattymango/lazycake-agent:latest", // a bare name resolves against docker.io/library and is denied
		"--pid=host --cap-add=SYS_ADMIN",             // tunnel tasks nsenter into sibling containers
		"-e LAZYCAKE_TOKEN=tok123",
		"-e LAZYCAKE_COORDINATOR_ADDR=coord.example:7443",
		"mkdir -p $HOME/.local/share/lazycake/bin",                           // -v fails if the host dir is missing
		"-v $HOME/.local/share/lazycake/bin:$HOME/.local/share/lazycake/bin", // same path on both sides
		"-e LAZYCAKE_LCINIT_HOST_DIR=$HOME/.local/share/lazycake/bin",        // so the agent stages lcinit there
		"/run/user/$(id -u)/podman/podman.sock",
		"--restart=always", // an agent that doesn't return after a reboot silently leaves the pool
		"--network=host",   // the agent reads the real link speed from the host's interfaces
		"-e LAZYCAKE_OFFER_CORES=2",
		"-e LAZYCAKE_OFFER_MEMORY_MB=2048",
		"-e LAZYCAKE_OFFER_DISK_MB=10240",
		"-e LAZYCAKE_OFFER_NETWORK_MBPS=100",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("install command is missing %q:\n%s", want, cmd)
		}
	}
}

func TestInstallCommandCarriesTheChosenOffer(t *testing.T) {
	cmd := (&Server{CoordinatorAddr: "c:7443"}).installCommand("t", machineOffer{Cores: 6, MemoryMB: 12288, DiskMB: 204800, NetworkMbps: 940})
	for _, want := range []string{"LAZYCAKE_OFFER_CORES=6 ", "LAZYCAKE_OFFER_MEMORY_MB=12288", "LAZYCAKE_OFFER_DISK_MB=204800", "LAZYCAKE_OFFER_NETWORK_MBPS=940"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("missing %s in:\n%s", want, cmd)
		}
	}
}

func TestMachineOfferValidation(t *testing.T) {
	if msg := defaultMachineOffer().validate(); msg != "" {
		t.Fatalf("the defaults must be valid: %s", msg)
	}
	// The top of the range is allowed and still fits the 32-bit megabyte fields the offer travels in.
	biggest := machineOffer{Cores: maxOfferCores, MemoryMB: maxOfferMemoryGB * mbPerGB, DiskMB: maxOfferDiskGB * mbPerGB, NetworkMbps: maxOfferMbps}
	if msg := biggest.validate(); msg != "" {
		t.Fatalf("the largest allowed offer must be valid: %s", msg)
	}
	if biggest.MemoryMB > math.MaxInt32 || biggest.DiskMB > math.MaxInt32 {
		t.Fatalf("the largest allowed offer overflows int32: %+v", biggest)
	}
	for name, mutate := range map[string]func(*machineOffer){
		"no cores":            func(o *machineOffer) { o.Cores = 0 },
		"negative cores":      func(o *machineOffer) { o.Cores = -4 },
		"too many cores":      func(o *machineOffer) { o.Cores = 5000 },
		"memory not whole GB": func(o *machineOffer) { o.MemoryMB = 2048 + 512 },
		"half a GB of memory": func(o *machineOffer) { o.MemoryMB = 512 },
		"absurd memory":       func(o *machineOffer) { o.MemoryMB = math.MaxInt32 },
		"storage not whole":   func(o *machineOffer) { o.DiskMB = 10*1024 + 1 },
		"no storage":          func(o *machineOffer) { o.DiskMB = 0 },
		"absurd storage":      func(o *machineOffer) { o.DiskMB = math.MaxInt32 },
		"negative net":        func(o *machineOffer) { o.NetworkMbps = -1 },
		"absurd network":      func(o *machineOffer) { o.NetworkMbps = 10_000_000 },
	} {
		o := defaultMachineOffer()
		mutate(&o)
		if o.validate() == "" {
			t.Errorf("%s must be rejected", name)
		}
	}
}
