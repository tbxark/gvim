package app

import (
	"os"
	"os/exec"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
)

// pluginIdleHeapLimit is the budget of the memory gate (FINAL decision D17): the default-
// enabled bundled plugins may hold at most this much heap while the editor is idle. The
// real cost is a fraction of it (the test logs the numbers); the margin is for the garbage
// collector's accounting and for the race detector, which makes every allocation larger.
const pluginIdleHeapLimit = 8 << 20

// pluginIdleHeapFloor is a sanity bound on the measurement: seven loaded Lua states cost
// several MiB, so a smaller reading means the baseline was polluted (heap that other tests
// left behind moves by more than that) and the number says nothing.
const pluginIdleHeapFloor = 1 << 20

// memGateChild marks the child process of TestBundledPluginsIdleHeap.
const memGateChild = "GVIM_TEST_MEMORY_GATE_CHILD"

// liveHeap is the heap in use after a full collection: what a plugin keeps while it is idle.
func liveHeap() uint64 {
	var ms goruntime.MemStats
	for range 3 {
		goruntime.GC()
	}
	goruntime.ReadMemStats(&ms)
	return ms.HeapAlloc
}

// TestBundledPluginsIdleHeap measures the idle heap of the default-enabled bundled plugin
// set: the editor with every bundled plugin that is on by default, loaded and idle with a
// file open, against the same editor started with --no-plugins. The difference is the cost
// of the plugins, which must stay under pluginIdleHeapLimit. When it does not, the largest
// bundled plugins have to become default_enabled:false (their plugin.json) and the docs
// (docs/plugins/bundled.md) must say so. The two sessions run in subtests so that the first
// is closed and collected before the second is measured.
//
// The live heap of a process is moved by every test that ran before (about 88 MiB after the
// whole package, with paused parallel tests moving it by several MiB between the two
// measurements), so the measurement runs in a fresh process: this test starts the test
// binary again, running only itself, and the child measures.
func TestBundledPluginsIdleHeap(t *testing.T) {
	if os.Getenv(memGateChild) != "" {
		measureBundledIdleHeap(t)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestBundledPluginsIdleHeap$", "-test.count=1", "-test.v", "-test.timeout=3m")
	cmd.Env = append(os.Environ(), memGateChild+"=1")
	out, err := cmd.CombinedOutput()
	for line := range strings.Lines(string(out)) {
		if strings.Contains(line, "idle heap") {
			t.Log(strings.TrimSpace(line))
		}
	}
	if err != nil {
		t.Fatalf("the measurement in a clean process failed: %v\n%s", err, out)
	}
}

// measureBundledIdleHeap is the measurement, run in the clean child process.
func measureBundledIdleHeap(t *testing.T) {
	var without, with uint64
	t.Run("without plugins", func(t *testing.T) {
		isolateConfig(t)
		s := newSession(t, Options{NoPlugins: true})
		t.Cleanup(s.rt.exit)
		s.openFile("a.go", "package a\n\nfunc main() {}\n")
		s.settle(50 * time.Millisecond)
		without = liveHeap()
	})
	var loaded []string
	t.Run("default bundled set", func(t *testing.T) {
		isolateConfig(t)
		s := newSession(t, Options{})
		t.Cleanup(s.rt.exit)
		s.until("every default-enabled bundled plugin to load", func() bool {
			n := 0
			for _, p := range s.plug.Plugins() {
				switch p.Status {
				case "loaded":
					n++
				case "loading":
					return false
				}
			}
			return n > 0
		})
		s.openFile("a.go", "package a\n\nfunc main() {}\n")
		s.settle(50 * time.Millisecond)
		for _, p := range s.plug.Plugins() {
			if p.Status == "loaded" {
				loaded = append(loaded, p.Name)
			}
		}
		with = liveHeap()
	})
	cost := int64(with) - int64(without)
	t.Logf("idle heap: %.2f MiB without plugins, %.2f MiB with %d plugins %v; plugins cost %.2f MiB (limit %d MiB)",
		float64(without)/(1<<20), float64(with)/(1<<20), len(loaded), loaded, float64(cost)/(1<<20), pluginIdleHeapLimit>>20)
	if len(loaded) == 0 {
		t.Fatal("no bundled plugin was loaded, so nothing was measured")
	}
	if cost < pluginIdleHeapFloor {
		t.Fatalf("the plugins cost %.2f MiB of heap, under the %d MiB sanity floor: the measurement is not trustworthy",
			float64(cost)/(1<<20), pluginIdleHeapFloor>>20)
	}
	if cost > pluginIdleHeapLimit {
		t.Fatalf("the default-enabled bundled plugins keep %.2f MiB of heap while idle, over the %d MiB limit: "+
			"set default_enabled to false in the plugin.json of the largest ones and update docs/plugins/bundled.md",
			float64(cost)/(1<<20), pluginIdleHeapLimit>>20)
	}
}
