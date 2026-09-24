package main

import (
	"fmt"

	"github.com/spf13/cobra"

	lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"
)

func newSubmitCmd() *cobra.Command {
	var (
		image          string
		cpu            float64
		memoryMB       int32
		diskMB         int32
		timeoutS       int32
		idempotencyKey string
		gatewayIDs     []string
	)

	cmd := &cobra.Command{
		Use:   "submit -- <command> [args...]",
		Short: "submit a task",
		Args:  cobra.MinimumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			if image == "" {
				return fmt.Errorf("--image is required")
			}
			client, ctx, closeFn, err := newClient()
			if err != nil {
				return err
			}
			defer closeFn()

			var entrypoint, cmdArgs []string
			if len(args) > 0 {
				entrypoint = []string{args[0]}
				cmdArgs = args[1:]
			}

			resp, err := client.SubmitTask(ctx, &lazycakev1.SubmitTaskRequest{
				Image:      image,
				Entrypoint: entrypoint,
				Args:       cmdArgs,
				Limits: &lazycakev1.TaskLimits{
					CpuCores: cpu, MemoryMb: memoryMB, DiskMb: diskMB, WallTimeoutS: timeoutS,
				},
				IdempotencyKey: idempotencyKey,
				GatewayIds:     gatewayIDs,
			})
			if err != nil {
				return err
			}
			fmt.Println(resp.GetTaskId())
			return nil
		},
	}

	cmd.Flags().StringVar(&image, "image", "", "digest-pinned image, e.g. alpine@sha256:...")
	cmd.Flags().Float64Var(&cpu, "cpu", 1, "CPU cores")
	cmd.Flags().Int32Var(&memoryMB, "memory", 512, "memory in MB")
	cmd.Flags().Int32Var(&diskMB, "disk", 1024, "disk in MB")
	cmd.Flags().Int32Var(&timeoutS, "timeout", 60, "wall timeout in seconds")
	cmd.Flags().StringVar(&idempotencyKey, "idempotency-key", "", "dedupe key for at_least_once retries")
	cmd.Flags().StringSliceVar(&gatewayIDs, "gateway", nil, "gateway ID the task may reach (repeatable, max 3)")

	return cmd
}
