// Command agent runs on a host's machine, executing containers under
// rootless Podman (or Docker) and reporting capacity to the coordinator.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mkassab215/lazycake/internal/version"
)

func main() {
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
