package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

func newGatewayCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "gateway",
		Short: "manage registered gateways",
	}

	var label string
	var services []string
	create := &cobra.Command{
		Use:   "create",
		Short: "register a new gateway and print its install token",
		RunE: func(cmd *cobra.Command, args []string) error {
			if label == "" {
				return fmt.Errorf("--label is required")
			}
			client, ctx, closeFn, err := newClient()
			if err != nil {
				return err
			}
			defer closeFn()

			resp, err := client.CreateGateway(ctx, &lazycakev1.CreateGatewayRequest{Label: label, Services: services})
			if err != nil {
				return err
			}
			fmt.Printf("gateway_id: %s\n", resp.GetGatewayId())
			fmt.Printf("install_token: %s\n", resp.GetInstallToken())
			fmt.Println()
			fmt.Println("Install on the server holding your data:")
			fmt.Printf("  LAZYCAKE_COORDINATOR_ADDR=<relay-addr> \\\n")
			fmt.Printf("  LAZYCAKE_TOKEN=%s \\\n", resp.GetInstallToken())
			fmt.Printf("  LAZYCAKE_GATEWAY_ID=%s \\\n", resp.GetGatewayId())
			svcFlag := ""
			for i, s := range services {
				if i > 0 {
					svcFlag += ","
				}
				svcFlag += s
			}
			fmt.Printf("  LAZYCAKE_SERVICES=%s \\\n", svcFlag)
			fmt.Println("  ./gateway")
			return nil
		},
	}
	create.Flags().StringVar(&label, "label", "", "human-readable name for this gateway")
	create.Flags().StringSliceVar(&services, "service", nil, "published service name:port (repeatable), e.g. db:5432")
	root.AddCommand(create)

	root.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "list registered gateways",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, ctx, closeFn, err := newClient()
			if err != nil {
				return err
			}
			defer closeFn()

			resp, err := client.ListGateways(ctx, &lazycakev1.ListGatewaysRequest{})
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "GATEWAY_ID\tLABEL\tCONNECTED\tSERVICES")
			for _, g := range resp.GetGateways() {
				fmt.Fprintf(w, "%s\t%s\t%v\t%s\n", g.GetGatewayId(), g.GetLabel(), g.GetConnected(), joinComma(g.GetServices()))
			}
			return w.Flush()
		},
	})

	return root
}

func joinComma(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}
