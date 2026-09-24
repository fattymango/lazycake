// Package probe implements the agent's preflight capability checks: rather
// than trust version strings, it actually exercises each enforcement
// mechanism (start a memory-capped container and confirm the OOM kill
// happens, etc.) so the agent only ever advertises a capability it proved
// it can enforce. See PLAN.md "Preflight capability probe".
package probe

import "fmt"

// Status is the outcome of one check.
type Status string

const (
	Pass    Status = "pass"
	Fail    Status = "fail"
	Skipped Status = "skipped"
)

// Result is one named check's outcome, with an actionable message on
// anything other than Pass.
type Result struct {
	Name   string
	Status Status
	Detail string
}

func pass(name string) Result { return Result{Name: name, Status: Pass} }

func fail(name, detail string, args ...any) Result {
	return Result{Name: name, Status: Fail, Detail: fmt.Sprintf(detail, args...)}
}

func skip(name, detail string, args ...any) Result {
	return Result{Name: name, Status: Skipped, Detail: fmt.Sprintf(detail, args...)}
}

// Report is every check's result plus the Capabilities the agent may
// therefore advertise.
type Report struct {
	Results []Result
}

// Passed reports whether the named check passed.
func (r Report) Passed(name string) bool {
	for _, res := range r.Results {
		if res.Name == name && res.Status == Pass {
			return true
		}
	}
	return false
}

// OK reports whether every check that must pass for the agent to run at
// all (memory and CPU enforcement) passed. Other checks degrade the
// advertised capability set but don't block startup.
func (r Report) OK() bool {
	return r.Passed(CheckMemoryLimit) && r.Passed(CheckCPUQuota)
}

// Check name constants, used both as map/result keys and in log output.
const (
	CheckCgroupVersion = "cgroup_version"
	CheckMemoryLimit   = "memory_limit"
	CheckCPUQuota      = "cpu_quota"
	CheckPIDsLimit     = "pids_limit"
	CheckDiskLimit     = "disk_limit"
	CheckSystemdSlice  = "systemd_slice"
	CheckGVisor        = "gvisor"
	CheckSubuid        = "subuid"
)
