// Command coordinator is the central server: owns Postgres, assigns tasks to
// agents, and relays encrypted tunnel traffic it cannot decrypt.
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
		fmt.Printf("coordinator %s\n", version.Version)
		return
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "coordinator:", err)
		os.Exit(1)
	}
}
