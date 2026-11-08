//go:build unix

package app

import (
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// openFDs counts the file descriptors the process holds.
func openFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/dev/fd")
	must(t, err)
	return len(entries)
}

// pluginGoroutines returns the stacks of the goroutines that run plugin code or the plugin
// OS: the runtime, a process or a watcher of the OS.
func pluginGoroutines() []string {
	buf := make([]byte, 4<<20)
	buf = buf[:goruntime.Stack(buf, true)]
	var out []string
	for _, g := range strings.Split(string(buf), "\n\n") {
		for _, marker := range []string{"gvim/plugin.", "app.(*osProc)", "app.(*osWatch)", "app.(*pluginOS)"} {
			if strings.Contains(g, marker) {
				out = append(out, g)
				break
			}
		}
	}
	return out
}

// processGone reports whether the process pid no longer runs. A killed child that nobody
// has waited for yet is a zombie, which counts as gone.
func processGone(pid int) bool {
	out, _ := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	st := strings.TrimSpace(string(out))
	return st == "" || strings.HasPrefix(st, "Z")
}

// TestQuittingUnloadsThePluginsCleanly: a plugin that holds a child process, a file watch and a
// repeating timer leaves nothing behind when the program ends: the child is killed, and no
// goroutine of the runtime or the plugin OS and no file descriptor outlives the exit.
func TestQuittingUnloadsThePluginsCleanly(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "pid")
	writeUserFile(t, "plugins/things.lua", `
local gvim = require "gvim"
gvim.command.register{ name = "start", title = "Start Things", run = function()
  gvim.proc.spawn({ "sh", "-c", "echo $$ > `+pidFile+`; exec sleep 60" }, {})
  gvim.fs.watch("`+dir+`", function() end)
  gvim.timer.every(20, function() end)
  gvim.state.set("started", true)
end }
gvim.on("shutdown", function() gvim.state.set("stopped", true) end)
`)
	s := newSession(t, Options{})
	s.waitStatus("things", "loaded")
	s.pump(50 * time.Millisecond)
	fdsBefore := openFDs(t)

	s.runCommand("Start Things")
	var pid int
	s.until("the child process to start", func() bool {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		return pid > 0
	})
	s.pump(100 * time.Millisecond) // the watch and the timer run
	if processGone(pid) {
		t.Fatalf("the child process %d is not running before the exit", pid)
	}
	if got := len(pluginGoroutines()); got == 0 {
		t.Fatal("no plugin goroutine runs before the exit, so the check below would prove nothing")
	}

	s.rt.exit()

	deadline := time.Now().Add(5 * time.Second)
	for !processGone(pid) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !processGone(pid) {
		t.Errorf("the child process %d still runs after the exit", pid)
	}
	var left []string
	for time.Now().Before(deadline) {
		if left = pluginGoroutines(); len(left) == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(left) > 0 {
		t.Errorf("%d plugin goroutines outlive the exit; the first:\n%s", len(left), left[0])
	}
	fdsAfter := openFDs(t)
	for fdsAfter > fdsBefore && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		fdsAfter = openFDs(t)
	}
	if fdsAfter > fdsBefore {
		t.Errorf("file descriptors: %d before the plugin started things, %d after the exit", fdsBefore, fdsAfter)
	}
	// The state the plugin wrote, including what its shutdown handler wrote, was flushed.
	data, err := os.ReadFile(filepath.Join(pluginConfigDir(), "plugin-data", "things", "state.json"))
	if err != nil || !strings.Contains(string(data), "stopped") || !strings.Contains(string(data), "started") {
		t.Errorf("plugin state after the exit = %q (%v), want started and stopped", data, err)
	}
}
