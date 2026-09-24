package main

import "github.com/spf13/cobra"

func newLogsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs <task_id>",
		Short: "show a task's logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented("logs")
		},
	}
	cmd.Flags().BoolP("follow", "f", false, "stream new log lines")
	return cmd
}
