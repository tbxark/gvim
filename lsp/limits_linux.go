//go:build linux

package lsp

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
)

// cgroupSeq numbers the cgroups of this process, one per started server.
var cgroupSeq atomic.Uint64

// cgroupBaseEnv overrides the cgroup v2 base the limits are created under, for tests and
// for a user whose delegation lives elsewhere.
const cgroupBaseEnv = "GVIM_CGROUP_BASE"

// PlanLimits builds the Linux plan for a server's process limits: a user-delegated cgroup v2
// of its own for memory.max and cpu.max (the child is moved in right after Start, and Release
// removes the cgroup once it has exited), and the sh ulimit -v
// wrapper for memory when no cgroup is delegated. A CPU percent cap has no rlimit (an rlimit
// CPU limit is seconds), so without a cgroup it is recorded as not applied.
func PlanLimits(l ProcessLimits) LimitPlan {
	if !l.Enabled || !l.Any() {
		return LimitPlan{}
	}
	configured := limitsSummary(l)
	var applied, unavailable []string
	var plan LimitPlan

	dir, cgroupOK := cgroupDir()
	if cgroupOK {
		if l.MaxMemoryPercent != nil {
			totalMB, err := totalMemoryMB()
			if err != nil {
				unavailable = append(unavailable, "memory: "+err.Error())
			} else if err := os.WriteFile(filepath.Join(dir, "memory.max"), []byte(strconv.FormatUint(memoryLimitBytes(*l.MaxMemoryPercent, totalMB), 10)), 0o644); err != nil {
				unavailable = append(unavailable, "memory: cgroup memory.max: "+err.Error())
			} else {
				applied = append(applied, fmt.Sprintf("cgroup memory %d%%", *l.MaxMemoryPercent))
				plan.Applied = true
			}
		}
		if l.MaxCPUPercent != nil {
			if err := os.WriteFile(filepath.Join(dir, "cpu.max"), []byte(cpuMaxQuota(*l.MaxCPUPercent)), 0o644); err != nil {
				unavailable = append(unavailable, "cpu: cgroup cpu.max: "+err.Error())
			} else {
				applied = append(applied, fmt.Sprintf("cgroup cpu %d%%", *l.MaxCPUPercent))
				plan.Applied = true
			}
		}
		if plan.Applied {
			plan.AfterStart = func(pid int) error {
				return os.WriteFile(filepath.Join(dir, "cgroup.procs"), []byte(strconv.Itoa(pid)), 0o644)
			}
			plan.Release = func() { _ = os.Remove(dir) }
		} else {
			_ = os.Remove(dir)
		}
	}

	// Memory fallback when no cgroup took it: RLIMIT_AS through sh, which is the only
	// pure-Go route left (os/exec has no pre-exec hook and SysProcAttr has no rlimits).
	if l.MaxMemoryPercent != nil && !containsPrefix(applied, "cgroup memory") {
		if totalMB, err := totalMemoryMB(); err == nil && shSupportsUlimitV() {
			plan.WrapMemoryKB = memoryLimitBytes(*l.MaxMemoryPercent, totalMB) / 1024
			applied = append(applied, fmt.Sprintf("sh ulimit -v %d KB memory", plan.WrapMemoryKB))
			plan.Applied = true
		} else {
			unavailable = append(unavailable, "memory: no delegated cgroup and sh ulimit -v unavailable")
		}
	}
	if l.MaxCPUPercent != nil && !containsPrefix(applied, "cgroup cpu") {
		unavailable = append(unavailable, "cpu: a percent cap needs a delegated cgroup (an rlimit CPU limit is seconds)")
	}
	plan.Note = limitNote(configured, applied, unavailable)
	return plan
}

// cgroupDir makes a cgroup for one server under a writable user-delegated cgroup v2 base,
// or reports that none is available. Each server gets its own, so the limits of one never
// apply to another. The locations of the base are Fresh's.
func cgroupDir() (string, bool) {
	base := os.Getenv(cgroupBaseEnv)
	if base == "" {
		root := "/sys/fs/cgroup"
		if _, err := os.Stat(root); err != nil {
			return "", false
		}
		uid := os.Getuid()
		for _, candidate := range []string{
			filepath.Join(root, fmt.Sprintf("user.slice/user-%d.slice/user@%d.service/app.slice", uid, uid)),
			filepath.Join(root, fmt.Sprintf("user.slice/user-%d.slice/user@%d.service", uid, uid)),
			filepath.Join(root, fmt.Sprintf("user.slice/user-%d.slice", uid)),
			filepath.Join(root, fmt.Sprintf("user-%d", uid)),
		} {
			if info, err := os.Stat(filepath.Join(candidate, "cgroup.procs")); err == nil && info.Mode().Perm()&0o200 != 0 {
				base = candidate
				break
			}
		}
		if base == "" {
			return "", false
		}
	}
	dir := filepath.Join(base, fmt.Sprintf("gvim-lsp-%d-%d", os.Getpid(), cgroupSeq.Add(1)))
	if err := os.Mkdir(dir, 0o755); err != nil && !os.IsExist(err) {
		return "", false
	}
	return dir, true
}

// totalMemoryMB is the system's memory from /proc/meminfo.
func totalMemoryMB() (uint64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, fmt.Errorf("reading /proc/meminfo: %w", err)
	}
	defer func() { _ = f.Close() }()
	return parseMemTotal(f)
}

// parseMemTotal reads MemTotal: N kB from a meminfo stream.
func parseMemTotal(r io.Reader) (uint64, error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			break
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("MemTotal: %w", err)
		}
		return kb / 1024, nil
	}
	if err := sc.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("no MemTotal in /proc/meminfo")
}

// shSupportsUlimitV probes whether sh can set RLIMIT_AS (ulimit -v).
func shSupportsUlimitV() bool {
	sh, err := exec.LookPath("sh")
	if err != nil {
		return false
	}
	return exec.Command(sh, "-c", "ulimit -v 65536 2>/dev/null").Run() == nil
}

// containsPrefix reports whether one of phrases starts with prefix.
func containsPrefix(phrases []string, prefix string) bool {
	for _, p := range phrases {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}
