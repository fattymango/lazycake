package main

import "github.com/spf13/cobra"

func newNodesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "nodes",
		Short: "list nodes known to the coordinator",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented("nodes")
		},
	}
}
