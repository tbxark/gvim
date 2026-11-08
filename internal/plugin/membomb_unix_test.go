//go:build unix && !race

package plugin_test

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/plugin"
	"github.com/tbxark/gvim/internal/plugin/plugintest"
)

// The peak resident memory of a plugin that allocates as fast as it can, under the editor's own
// heap guard. A single `..` is one instruction of gopher-lua's VM that nothing can interrupt, so
// the guard stops the slice after the instruction that crossed the limit and the peak is the limit
// plus what that instruction allocated. Each case runs in a process of its own, because the peak
// resident size of a test process is the largest of everything it ran. docs/plugins/limits.md
// lists the numbers.

const memBombEnv = "GVIM_MEMBOMB_CASE"

var memBombs = map[string]string{
	"double":       `local s = "x" while true do s = s .. s end`,
	"double60":     `local s = string.rep("x", 60 * 1024 * 1024) while true do s = s .. s end`,
	"double33":     `local s = string.rep("x", 33 * 1024 * 1024) while true do s = s .. s end`,
	"retain":       `local t = {} while true do t[#t + 1] = string.rep("x", 1048576) .. #t end`,
	"concat4":      `local s = string.rep("x", 1048576) while true do s = s .. s .. s .. s end`,
	"table.concat": `local t = {} for i = 1, 40 do t[i] = string.rep("x", 60 * 1024 * 1024) end T.spy(#table.concat(t))`,
	"byte":         `local s = string.rep("x", 60 * 1024 * 1024) local t = {s:byte(1, -1)} T.spy(#t)`,
	"gsub":         `local s = string.rep("x", 1024 * 1024) local o = s:gsub("x", string.rep("y", 1024 * 1024)) T.spy(#o)`,
	"format":       `local s = string.rep("x", 60 * 1024 * 1024) T.spy(#string.format("%s%s%s%s%s%s%s%s", s, s, s, s, s, s, s, s))`,
	"rep":          `T.spy(#string.rep(string.rep("x", 60 * 1024 * 1024), 100))`,
	"unpack":       `local x = {unpack({}, 1, 100000000)} T.spy(#x)`,
	"sparse":       `local t = {} t[1000000000] = 1 t[1048576] = 1 T.spy(#t)`,
}

// TestMemBombChild is the child process of TestPeakMemoryUnderTheHeapGuard.
func TestMemBombChild(t *testing.T) {
	name := os.Getenv(memBombEnv)
	if name == "" {
		t.Skip("child of TestPeakMemoryUnderTheHeapGuard")
	}
	h := start(t, `T.command("go", function() `+memBombs[name]+` end)`,
		plugintest.Options{Timeout: 20 * time.Second, EventBudget: 20 * time.Second, HeapLimit: plugin.DefaultHeapLimit})
	_ = h.Run("t.go", nil)
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		t.Fatal(err)
	}
	peak := int64(ru.Maxrss)
	if runtime.GOOS != "darwin" {
		peak <<= 10 // kilobytes elsewhere
	}
	fmt.Printf("PEAK_RSS=%d\n", peak)
}

func TestPeakMemoryUnderTheHeapGuard(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates memory")
	}
	const (
		g    = plugin.DefaultHeapLimit
		base = 40 << 20 // a Go test process before it does anything
	)
	tests := []struct {
		name string
		max  int64 // the largest peak, in bytes
	}{
		// a string that doubles: the limit again for the instruction that was in flight
		{"double", 2*g + g/4 + base},
		{"double60", 2*g + base},
		{"double33", 2*g + base},
		// retained growth is what the guard exists for, and it is seen long before it matters
		{"retain", 3*g/2 + base},
		// the residual: every operand of one `..` counts, here four, and the instruction
		// allocates all of them at once
		{"concat4", 3*g + base},
		// every other single call that could make a large string is capped by the runtime
		{"table.concat", 3*g + base},
		{"byte", g/2 + base},
		{"gsub", 2*g + base},
		{"format", g/2 + base},
		{"rep", g/2 + base},
		{"unpack", base},
		{"sparse", base},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The guard is a goroutine that looks every millisecond. On a machine that is busy with
			// other work (the rest of go test ./...) it can look late, and the peak is then larger: the
			// best of three runs is what the guard does when it gets the CPU.
			var peak int64
			for range 3 {
				cmd := exec.Command(os.Args[0], "-test.run=^TestMemBombChild$")
				cmd.Env = append(os.Environ(), memBombEnv+"="+tt.name)
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("child: %v\n%s", err, out)
				}
				got := int64(0)
				for _, line := range strings.Split(string(out), "\n") {
					if v, ok := strings.CutPrefix(line, "PEAK_RSS="); ok {
						got, _ = strconv.ParseInt(v, 10, 64)
					}
				}
				if got == 0 {
					t.Fatalf("the child reported no peak:\n%s", out)
				}
				if peak == 0 || got < peak {
					peak = got
				}
				if peak <= tt.max {
					break
				}
			}
			t.Logf("peak resident size %d MiB (limit %d MiB, bound %d MiB)", peak>>20, int64(g)>>20, tt.max>>20)
			if peak > tt.max {
				t.Errorf("peak resident size %d MiB, want at most %d MiB", peak>>20, tt.max>>20)
			}
		})
	}
}
