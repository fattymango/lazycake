package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

func newLogsCmd() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs <task_id>",
		Short: "show a task's logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, ctx, closeFn, err := newClient()
			if err != nil {
				return err
			}
			defer closeFn()

			stream, err := client.StreamLogs(ctx, &lazycakev1.StreamLogsRequest{TaskId: args[0], Follow: follow})
			if err != nil {
				return err
			}
			for {
				line, err := stream.Recv()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return err
				}
				fmt.Println(line.GetLine())
			}
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "stream new log lines")
	return cmd
}
