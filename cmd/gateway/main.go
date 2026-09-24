// Command gateway runs on a customer's server, terminating the Noise tunnel
// and forwarding connections to local services it explicitly publishes.
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
		fmt.Printf("gateway %s\n", version.Version)
		return
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gateway:", err)
		os.Exit(1)
	}
}
