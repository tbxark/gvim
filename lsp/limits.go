package lsp

import (
	"fmt"
	"strings"
)

// ProcessLimits is one language server's process_limits (Fresh's shape): a memory cap as a
// percent of system memory, a CPU cap where 100 is one core, and the switch that says
// whether Fresh would apply them. A zero value applies nothing.
type ProcessLimits struct {
	Enabled          bool
	MaxMemoryPercent *uint32
	MaxCPUPercent    *uint32
}

// Any reports whether a limit is configured. Enabled with no cap applies nothing.
func (l ProcessLimits) Any() bool {
	return l.MaxMemoryPercent != nil || l.MaxCPUPercent != nil
}

// LimitPlan is what Start can do about a server's process limits: rewrite the command
// line (the sh ulimit memory fallback), run an action once the child exists (the cgroup
// move), and the note the server status shows. It never fails Start: a limit that cannot
// be applied is recorded in Note instead of being dropped silently.
type LimitPlan struct {
	// WrapMemoryKB wraps the command in sh -c 'ulimit -v KB; exec "$@"' when > 0.
	WrapMemoryKB uint64
	// AfterStart runs with the child pid once cmd.Start returned; the cgroup move.
	AfterStart func(pid int) error
	// Release frees what the plan made for this one server (its cgroup). The caller runs
	// it once the child has exited, or when it will not start one. It is nil when the
	// plan made nothing.
	Release func()
	// Note says what applies, or why nothing does. Empty without configured limits.
	Note string
	// Applied reports that at least one configured limit took effect.
	Applied bool
}

// memoryLimitBytes is percent of totalMB as a byte count.
func memoryLimitBytes(percent uint32, totalMB uint64) uint64 {
	return totalMB * uint64(percent) / 100 * 1024 * 1024
}

// cpuMaxQuota renders a cgroup v2 cpu.max value for percent, where 100 is one core and
// 200 means two cores. The period is the standard 100 ms.
func cpuMaxQuota(percent uint32) string {
	const periodUS = 100_000
	quota := uint64(periodUS) * uint64(percent) / 100
	return fmt.Sprintf("%d %d", quota, periodUS)
}

// limitsSummary renders the configured caps, for a note.
func limitsSummary(l ProcessLimits) string {
	var parts []string
	if l.MaxMemoryPercent != nil {
		parts = append(parts, fmt.Sprintf("memory %d%% of system memory", *l.MaxMemoryPercent))
	}
	if l.MaxCPUPercent != nil {
		parts = append(parts, fmt.Sprintf("cpu %d%% (100%% = one core)", *l.MaxCPUPercent))
	}
	return strings.Join(parts, ", ")
}

// limitNote joins what applied and what did not, for the server status. configured is the
// requested caps; applied and unavailable are the short method/reason phrases.
func limitNote(configured string, applied, unavailable []string) string {
	var parts []string
	if len(applied) > 0 {
		parts = append(parts, "applied: "+strings.Join(applied, ", "))
	}
	if len(unavailable) > 0 {
		parts = append(parts, "not applied: "+strings.Join(unavailable, ", "))
	}
	if len(parts) == 0 {
		return ""
	}
	return "process_limits (" + configured + "): " + strings.Join(parts, "; ")
}
