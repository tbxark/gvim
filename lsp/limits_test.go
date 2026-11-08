package lsp

import (
	"runtime"
	"strings"
	"testing"
)

// TestProcessLimitMath checks the percent-to-value math: memory is a share of the system
// memory in bytes, and the CPU share becomes the cgroup v2 cpu.max quota of a 100 ms
// period (100 = one core, 200 = two).
func TestProcessLimitMath(t *testing.T) {
	if got := memoryLimitBytes(50, 16_000); got != 8_000*1024*1024 {
		t.Fatalf("memoryLimitBytes(50, 16000) = %d, want 8 GiB", got)
	}
	if got := memoryLimitBytes(1, 100); got != 1*1024*1024 {
		t.Fatalf("memoryLimitBytes(1, 100) = %d, want 1 MiB", got)
	}
	tests := []struct {
		percent uint32
		want    string
	}{
		{100, "100000 100000"},
		{90, "90000 100000"},
		{200, "200000 100000"},
		{1, "1000 100000"},
	}
	for _, tt := range tests {
		if got := cpuMaxQuota(tt.percent); got != tt.want {
			t.Errorf("cpuMaxQuota(%d) = %q, want %q", tt.percent, got, tt.want)
		}
	}
}

// TestLimitsSummaryAndNote checks the status wording: the configured caps and what applied
// or could not.
func TestLimitsSummaryAndNote(t *testing.T) {
	mem, cpu := uint32(50), uint32(90)
	limits := ProcessLimits{Enabled: true, MaxMemoryPercent: &mem, MaxCPUPercent: &cpu}
	if got := limitsSummary(limits); got != "memory 50% of system memory, cpu 90% (100% = one core)" {
		t.Fatalf("summary = %q", got)
	}
	note := limitNote(limitsSummary(limits), []string{"cgroup memory 50%"}, []string{"cpu: no cgroup"})
	for _, want := range []string{"process_limits", "memory 50%", "cpu 90%", "applied: cgroup memory 50%", "not applied: cpu: no cgroup"} {
		if !strings.Contains(note, want) {
			t.Errorf("note = %q, want %q", note, want)
		}
	}
}

// TestLimitPlanNothingConfigured checks that a zero or cap-less limit plans nothing at all:
// no wrapping, no note.
func TestLimitPlanNothingConfigured(t *testing.T) {
	for _, limits := range []ProcessLimits{
		{},
		{Enabled: true},
		{Enabled: false, MaxMemoryPercent: new(uint32)},
	} {
		plan := PlanLimits(limits)
		if plan.Applied || plan.Note != "" || plan.WrapMemoryKB != 0 || plan.AfterStart != nil {
			t.Errorf("PlanLimits(%+v) = %+v, want the zero plan", limits, plan)
		}
	}
}

// TestPlanLimitsRecordsAnUnsupportedPlatform checks the note on a platform where the limits
// cannot be applied: the configured caps are named, the platform is named, and the reason is
// recorded instead of the limits being silently dropped.
func TestPlanLimitsRecordsAnUnsupportedPlatform(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("the Linux plan is exercised by the linux-tagged tests")
	}
	mem, cpu := uint32(50), uint32(90)
	plan := PlanLimits(ProcessLimits{Enabled: true, MaxMemoryPercent: &mem, MaxCPUPercent: &cpu})
	if plan.Applied || plan.WrapMemoryKB != 0 || plan.AfterStart != nil {
		t.Fatalf("plan = %+v, want nothing applicable", plan)
	}
	for _, want := range []string{"process_limits", "memory 50%", "cpu 90%", "not applied on " + runtime.GOOS, "Fresh also applies them on Linux only"} {
		if !strings.Contains(plan.Note, want) {
			t.Errorf("note = %q, want %q", plan.Note, want)
		}
	}
}
