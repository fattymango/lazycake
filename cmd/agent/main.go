// Command agent runs on a host's machine, executing containers under
// rootless Podman (or Docker) and reporting capacity to the coordinator.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mkassab215/lazycake/internal/agent/netns"
	"github.com/mkassab215/lazycake/internal/version"
)

func main() {
	// Checked before flag.Parse(): this process is always started by
	// nsenter (see internal/agent/netns.Proxy.Setup), never typed by a
	// human, so it skips normal flag handling entirely.
	if len(os.Args) > 1 && os.Args[1] == netns.SubcommandName {
		if err := netns.ServeSubcommand(); err != nil {
			fmt.Fprintln(os.Stderr, "agent "+netns.SubcommandName+":", err)
			os.Exit(1)
		}
		return
	}

	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("agent %s\n", version.Version)
		return
	}

	args := flag.Args()
	if len(args) > 0 && args[0] == "probe" {
		if err := runProbe(); err != nil {
			fmt.Fprintln(os.Stderr, "agent probe:", err)
			os.Exit(1)
		}
		return
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "agent:", err)
		os.Exit(1)
	}
}
