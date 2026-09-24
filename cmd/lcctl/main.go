// Command lcctl is the customer-facing CLI for submitting and inspecting
// tasks against a LazyCake coordinator.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mkassab215/lazycake/internal/version"
)

func main() {
	root := &cobra.Command{
		Use:     "lcctl",
		Short:   "LazyCake customer CLI",
		Version: version.Version,
	}

	root.AddCommand(newSubmitCmd())
	root.AddCommand(newStatusCmd())
	root.AddCommand(newLogsCmd())
	root.AddCommand(newNodesCmd())
	root.AddCommand(newGatewayCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "lcctl:", err)
		os.Exit(1)
	}
}
