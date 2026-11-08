//go:build !linux

package lsp

import (
	"fmt"
	"runtime"
)

// PlanLimits builds the plan for a server's process limits on a platform where they cannot
// be applied. Go's os/exec has no pre-exec hook and syscall.SysProcAttr has no rlimits, so a
// pure-Go binary cannot set RLIMIT_AS for a child; without Linux cgroup v2 a CPU percentage
// has no equivalent either. Fresh applies process_limits on Linux only and leaves them
// unapplied here too. The reason is recorded in the server status, never dropped silently.
func PlanLimits(l ProcessLimits) LimitPlan {
	if !l.Enabled || !l.Any() {
		return LimitPlan{}
	}
	return LimitPlan{Note: fmt.Sprintf(
		"process_limits (%s): not applied on %s: the limits need Linux cgroup v2 (Go's os/exec has no pre-exec rlimit hook); Fresh also applies them on Linux only",
		limitsSummary(l), runtime.GOOS)}
}
