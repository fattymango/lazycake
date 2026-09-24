package probe

import "context"

// fakeRunner scripts CmdResult/error responses keyed by the subcommand
// (args[0], e.g. "run", "inspect", "mkfs.ext4"), popped in FIFO order per
// key so a check that runs the same subcommand twice (capped vs uncapped
// CPU probes) can be scripted with two different outcomes.
type fakeRunner struct {
	queues map[string][]CmdResult
	errs   map[string][]error
	calls  []string
}

func newFakeRunner() *fakeRunner {
	return &fakeRunner{queues: map[string][]CmdResult{}, errs: map[string][]error{}}
}

func (f *fakeRunner) on(subcmd string, res CmdResult) *fakeRunner {
	f.queues[subcmd] = append(f.queues[subcmd], res)
	return f
}

func (f *fakeRunner) onErr(subcmd string, err error) *fakeRunner {
	f.errs[subcmd] = append(f.errs[subcmd], err)
	return f
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) (CmdResult, error) {
	key := name
	if name == "podman" && len(args) > 0 {
		key = args[0]
	}
	f.calls = append(f.calls, key)

	if errs, ok := f.errs[key]; ok && len(errs) > 0 {
		err := errs[0]
		f.errs[key] = errs[1:]
		return CmdResult{}, err
	}
	if q, ok := f.queues[key]; ok && len(q) > 0 {
		res := q[0]
		f.queues[key] = q[1:]
		return res, nil
	}
	return CmdResult{}, nil
}
