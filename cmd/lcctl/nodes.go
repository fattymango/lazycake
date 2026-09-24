package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

func newNodesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "nodes",
		Short: "list nodes known to the coordinator",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, ctx, closeFn, err := newClient()
			if err != nil {
				return err
			}
			defer closeFn()

			resp, err := client.ListNodes(ctx, &lazycakev1.ListNodesRequest{})
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "NODE_ID\tHOSTNAME\tCONNECTED\tCORES\tMEMORY_MB\tDISK_MB\tTRUST")
			for _, n := range resp.GetNodes() {
				fmt.Fprintf(w, "%s\t%s\t%v\t%.2f\t%d\t%d\t%.2f\n",
					n.GetNodeId(), n.GetHostname(), n.GetConnected(),
					n.GetOfferCores(), n.GetOfferMemoryMb(), n.GetOfferDiskMb(), n.GetTrustScore())
			}
			return w.Flush()
		},
	}
}
