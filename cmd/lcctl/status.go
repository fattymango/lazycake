package main

import (
	"fmt"

	"github.com/spf13/cobra"

	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <task_id>",
		Short: "show a task's status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, ctx, closeFn, err := newClient()
			if err != nil {
				return err
			}
			defer closeFn()

			st, err := client.GetTask(ctx, &lazycakev1.GetTaskRequest{TaskId: args[0]})
			if err != nil {
				return err
			}

			fmt.Printf("task_id:  %s\n", st.GetTaskId())
			fmt.Printf("state:    %s\n", st.GetState())
			if st.GetNodeId() != "" {
				fmt.Printf("node_id:  %s\n", st.GetNodeId())
			}
			if st.GetHasExitCode() {
				fmt.Printf("exit:     %d (%s)\n", st.GetExitCode(), st.GetExitReason())
			}
			return nil
		},
	}
}
