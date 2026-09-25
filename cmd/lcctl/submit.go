package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

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
		targetSpecs    []string
		count          int
	)

	cmd := &cobra.Command{
		Use:   "submit -- <command> [args...]",
		Short: "submit a task (or, with --count, fan out N identical ones)",
		Args:  cobra.MinimumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			if image == "" {
				return fmt.Errorf("--image is required")
			}
			if count < 1 {
				return fmt.Errorf("--count must be >= 1")
			}
			targets, err := parseTargetSpecs(targetSpecs)
			if err != nil {
				return err
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

			req := &lazycakev1.SubmitTaskRequest{
				Image:      image,
				Entrypoint: entrypoint,
				Args:       cmdArgs,
				Limits: &lazycakev1.TaskLimits{
					CpuCores: cpu, MemoryMb: memoryMB, DiskMb: diskMB, WallTimeoutS: timeoutS,
				},
				Targets: targets,
			}

			if count == 1 {
				req.IdempotencyKey = idempotencyKey
				resp, err := client.SubmitTask(ctx, req)
				if err != nil {
					return err
				}
				fmt.Println(resp.GetTaskId())
				return nil
			}
			return submitFanout(ctx, client, req, idempotencyKey, count)
		},
	}

	cmd.Flags().StringVar(&image, "image", "", "digest-pinned image, e.g. alpine@sha256:...")
	cmd.Flags().Float64Var(&cpu, "cpu", 1, "CPU cores")
	cmd.Flags().Int32Var(&memoryMB, "memory", 512, "memory in MB")
	cmd.Flags().Int32Var(&diskMB, "disk", 1024, "disk in MB")
	cmd.Flags().Int32Var(&timeoutS, "timeout", 60, "wall timeout in seconds")
	cmd.Flags().StringVar(&idempotencyKey, "idempotency-key", "", "dedupe key for at_least_once retries (ignored with --count > 1: each fanned-out task gets its own key)")
	cmd.Flags().StringSliceVar(&targetSpecs, "target", nil,
		"gateway_id:hostname:port the task may reach (repeatable, max 3), e.g. gw_abc:db.acme.com:5432")
	cmd.Flags().IntVar(&count, "count", 1,
		"submit this many identical tasks concurrently (task 6.3: a single command fanning out across the fleet)")

	return cmd
}

// submitFanout submits count copies of req concurrently (bounded so this
// doesn't open hundreds of simultaneous gRPC calls for a very large
// --count) and prints each task ID as it comes back, in submission order,
// so the output is still useful piped into something even though the
// underlying calls complete out of order.
func submitFanout(ctx context.Context, client lazycakev1.CustomerServiceClient, base *lazycakev1.SubmitTaskRequest, idempotencyPrefix string, count int) error {
	const maxConcurrent = 20
	sem := make(chan struct{}, maxConcurrent)
	results := make([]string, count)
	errs := make([]error, count)

	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()

			// A field-by-field copy, not *base: SubmitTaskRequest embeds a
			// protobuf MessageState (a sync.Mutex among other things), so
			// copying the struct itself - even read-only, even once, even
			// before any concurrent use - is exactly what `go vet`'s
			// copylocks check exists to catch.
			req := &lazycakev1.SubmitTaskRequest{
				Image: base.Image, Entrypoint: base.Entrypoint, Args: base.Args,
				Env: base.Env, Workdir: base.Workdir, Limits: base.Limits,
				Targets: base.Targets, Delivery: base.Delivery,
			}
			if idempotencyPrefix != "" {
				req.IdempotencyKey = fmt.Sprintf("%s-%d", idempotencyPrefix, i)
			}
			resp, err := client.SubmitTask(ctx, req)
			if err != nil {
				errs[i] = err
				return
			}
			results[i] = resp.GetTaskId()
		}(i)
	}
	wg.Wait()

	failed := 0
	for i, id := range results {
		if errs[i] != nil {
			failed++
			fmt.Fprintf(os.Stderr, "task %d/%d failed: %v\n", i+1, count, errs[i])
			continue
		}
		fmt.Println(id)
	}
	if failed > 0 {
		return fmt.Errorf("%d/%d submissions failed", failed, count)
	}
	return nil
}

func parseTargetSpecs(specs []string) ([]*lazycakev1.TunnelTargetSpec, error) {
	out := make([]*lazycakev1.TunnelTargetSpec, 0, len(specs))
	for _, spec := range specs {
		parts := strings.SplitN(spec, ":", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("malformed --target %q, want gateway_id:hostname:port", spec)
		}
		port, err := strconv.Atoi(parts[2])
		if err != nil || port <= 0 {
			return nil, fmt.Errorf("malformed port in --target %q", spec)
		}
		out = append(out, &lazycakev1.TunnelTargetSpec{
			GatewayId: parts[0], Hostname: parts[1], Port: int32(port),
		})
	}
	return out, nil
}
