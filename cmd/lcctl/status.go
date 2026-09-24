package main

import "github.com/spf13/cobra"

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <task_id>",
		Short: "show a task's status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented("status")
		},
	}
}
