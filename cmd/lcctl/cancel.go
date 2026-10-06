package main

import (
	"fmt"

	"github.com/spf13/cobra"

	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

func newCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <task_id>",
		Short: "stop a task",
		Long: "Stops a task. A queued task is cancelled at once and costs nothing. A running task is told to stop\n" +
			"and finishes shortly after (check with `lcctl status`); you are charged only for the time it ran.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, ctx, closeFn, err := newClient()
			if err != nil {
				return err
			}
			defer closeFn()

			st, err := client.CancelTask(ctx, &lazycakev1.CancelTaskRequest{TaskId: args[0]})
			if err != nil {
				return err
			}
			if st.GetCancelRequested() {
				fmt.Printf("%s: stopping (state %s); check `lcctl status %s`\n", st.GetTaskId(), st.GetState(), st.GetTaskId())
			} else {
				fmt.Printf("%s: %s (%s)\n", st.GetTaskId(), st.GetState(), st.GetExitReason())
			}
			return nil
		},
	}
}
