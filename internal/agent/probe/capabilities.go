package probe

import lazycakev1 "github.com/mkassab215/lazycake/internal/proto/lazycake/v1"

// Capabilities converts a Report into the wire message the agent presents
// at registration. Runtime and CgroupVersion are always filled in;
// everything else defaults false unless its check passed - the agent never
// advertises a capability it did not itself verify.
func (r Report) Capabilities(runtime string) *lazycakev1.Capabilities {
	cgroupVersion := "v1"
	if r.Passed(CheckCgroupVersion) {
		cgroupVersion = "v2"
	}
	return &lazycakev1.Capabilities{
		MemoryLimit:   r.Passed(CheckMemoryLimit),
		CpuQuota:      r.Passed(CheckCPUQuota),
		PidsLimit:     r.Passed(CheckPIDsLimit),
		DiskLimit:     r.Passed(CheckDiskLimit),
		Gvisor:        r.Passed(CheckGVisor),
		SystemdSlice:  r.Passed(CheckSystemdSlice),
		Runtime:       runtime,
		CgroupVersion: cgroupVersion,
	}
}
