package main

import "github.com/spf13/cobra"

func newGatewayCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "gateway",
		Short: "manage registered gateways",
	}
	root.AddCommand(&cobra.Command{
		Use:   "create",
		Short: "register a new gateway and print its install token",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented("gateway create")
		},
	})
	root.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "list registered gateways",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errNotImplemented("gateway list")
		},
	})
	return root
}
