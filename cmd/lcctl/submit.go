package main

import "github.com/spf13/cobra"

func newSubmitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "submit",
		Short: "submit a task",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented("submit")
		},
	}
}
