//go:build linux

package lsp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlanLimitsCgroupAndPidMove checks the Linux cgroup plan with a delegated base: the
// per-editor cgroup gets memory.max and cpu.max, the note names the methods that applied,
// and AfterStart moves the child into the cgroup.
func TestPlanLimitsCgroupAndPidMove(t *testing.T) {
	base := t.TempDir()
	t.Setenv(cgroupBaseEnv, base)
	mem, cpu := uint32(50), uint32(90)
	plan := PlanLimits(ProcessLimits{Enabled: true, MaxMemoryPercent: &mem, MaxCPUPercent: &cpu})
	if !plan.Applied || plan.AfterStart == nil {
		t.Fatalf("plan = %+v, want an applied cgroup plan", plan)
	}
	for _, want := range []string{"cgroup memory 50%", "cgroup cpu 90%"} {
		if !strings.Contains(plan.Note, want) {
			t.Errorf("note = %q, want %q", plan.Note, want)
		}
	}
	dir := filepath.Join(base, fmt.Sprintf("gvim-lsp-%d", os.Getpid()))
	total, err := totalMemoryMB()
	if err != nil {
		t.Fatalf("totalMemoryMB: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "memory.max"))
	if err != nil {
		t.Fatalf("memory.max: %v", err)
	}
	if got, want := strings.TrimSpace(string(data)), fmt.Sprint(memoryLimitBytes(50, total)); got != want {
		t.Fatalf("memory.max = %q, want %q", got, want)
	}
	data, err = os.ReadFile(filepath.Join(dir, "cpu.max"))
	if err != nil {
		t.Fatalf("cpu.max: %v", err)
	}
	if got, want := strings.TrimSpace(string(data)), cpuMaxQuota(90); got != want {
		t.Fatalf("cpu.max = %q, want %q", got, want)
	}
	if err := plan.AfterStart(4321); err != nil {
		t.Fatalf("AfterStart: %v", err)
	}
	data, err = os.ReadFile(filepath.Join(dir, "cgroup.procs"))
	if err != nil {
		t.Fatalf("cgroup.procs: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "4321" {
		t.Fatalf("cgroup.procs = %q, want the child pid", got)
	}
}

// TestPlanLimitsWithoutACgroupRecordsTheFallbacks checks the no-cgroup path: memory falls
// back to the sh ulimit wrapper when sh supports it, and the CPU percent (which has no
// rlimit) is recorded as not applied.
func TestPlanLimitsWithoutACgroupRecordsTheFallbacks(t *testing.T) {
	t.Setenv(cgroupBaseEnv, filepath.Join(t.TempDir(), "missing", "base"))
	mem, cpu := uint32(50), uint32(90)
	plan := PlanLimits(ProcessLimits{Enabled: true, MaxMemoryPercent: &mem, MaxCPUPercent: &cpu})
	if !strings.Contains(plan.Note, "cpu: a percent cap needs a delegated cgroup") {
		t.Fatalf("note = %q, want the CPU reason", plan.Note)
	}
	if shSupportsUlimitV() {
		if plan.WrapMemoryKB == 0 || !strings.Contains(plan.Note, "sh ulimit -v") {
			t.Fatalf("plan = %+v, want the sh ulimit memory fallback", plan)
		}
	}
}

// TestParseMemTotal checks the /proc/meminfo reading.
func TestParseMemTotal(t *testing.T) {
	got, err := parseMemTotal(strings.NewReader("MemFree: 1 kB\nMemTotal:       16384000 kB\n"))
	if err != nil || got != 16_000 {
		t.Fatalf("parseMemTotal = %d, %v; want 16000 MB", got, err)
	}
	if _, err := parseMemTotal(strings.NewReader("MemFree: 1 kB\n")); err == nil {
		t.Fatal("a meminfo without MemTotal parsed")
	}
}
