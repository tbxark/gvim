package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/config"
	"github.com/tbxark/gvim/internal/pluginapi"
)

func TestLogFileWritesNoticesAndPluginLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gvim.log")
	l, err := openLogFile(path)
	if err != nil {
		t.Fatal(err)
	}
	l.Notice("Saved a.txt")
	l.Plugin(pluginapi.LogEntry{Plugin: "hello", Level: "warn", Msg: "slow\nsecond line"})
	l.Plugin(pluginapi.LogEntry{Level: "info", Msg: "3 plugin(s) loaded"})
	// the file is current while the editor runs, not only at exit
	deadline := time.Now().Add(3 * time.Second)
	var got string
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(path)
		if got = string(b); strings.Contains(got, "3 plugin(s) loaded") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, want := range []string{" notice Saved a.txt\n", " plugin(hello) warn slow | second line\n", " plugin info 3 plugin(s) loaded\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("log lacks %q:\n%s", want, got)
		}
	}
	l.Close()
	l.Notice("after the close") // must not panic or write
	b, _ := os.ReadFile(path)
	if !strings.HasSuffix(strings.TrimSpace(string(b)), "gvim exited") || strings.Contains(string(b), "after the close") {
		t.Errorf("the log after Close:\n%s", b)
	}
	// a second run appends
	l2, err := openLogFile(path)
	if err != nil {
		t.Fatal(err)
	}
	l2.Close()
	b2, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(b2), string(b)) || len(b2) <= len(b) {
		t.Errorf("a second run did not append:\n%s", b2)
	}
}

// An unwritable path is a notice, not a failure, and a nil log ignores everything.
func TestLogFileUnwritableIsSafe(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "no", "such", "dir", "gvim.log")
	l, notice := startLogFile(bad, nil)
	if l != nil || !strings.Contains(notice, "cannot write the log file") || !strings.Contains(notice, bad) {
		t.Fatalf("log %v, notice %q", l, notice)
	}
	l.Notice("x")
	l.Plugin(pluginapi.LogEntry{Msg: "x"})
	l.Close()
	if l, notice := startLogFile("", nil); l != nil || notice != "" {
		t.Fatalf("no path: %v %q", l, notice)
	}
}

// A full queue drops lines instead of blocking the update loop.
func TestLogFileNeverBlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gvim.log")
	l, err := openLogFile(path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		for range 50_000 {
			l.Notice("line")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("writing blocked")
	}
	l.Close()
}

func TestStartLogFileLogsConfigProblems(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gvim.log")
	l, notice := startLogFile(path, []config.Diagnostic{{Message: "bad key"}})
	if l == nil || notice != "" {
		t.Fatalf("log %v notice %q", l, notice)
	}
	l.Close()
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), " config ") || !strings.Contains(string(b), "bad key") {
		t.Fatalf("the config problem is not in the log:\n%s", b)
	}
}

// TestLogFileEndToEnd: with --log-file the plugin log lines and the notices of the session reach
// the file, and a broken plugin's error is one of them.
func TestLogFileEndToEnd(t *testing.T) {
	isolateConfig(t)
	writeUserFile(t, "plugins/hello.lua", `local gvim = require "gvim"
gvim.log.warn("hello from the plugin")
local x = nil
x.y = 1`)
	path := filepath.Join(t.TempDir(), "gvim.log")
	s := newSession(t, Options{LogFile: path})
	s.waitStatus("hello", "failed")
	s.settle(50 * time.Millisecond)
	s.rt.close() // flushes and closes the file; the cleanup closes it again, which is harmless
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{" plugin(hello) warn hello from the plugin", " plugin(hello) error load failed: hello/hello.lua:4: ", " notice "} {
		if !strings.Contains(got, want) {
			t.Errorf("log lacks %q:\n%s", want, got)
		}
	}
}

// Each line the editor writes reaches the file, in order, once it is closed.
func TestFileLogWritesEveryLineAtOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gvim.log")
	l, err := openLogFile(path)
	if err != nil {
		t.Fatal(err)
	}
	l.Notice("first notice")
	l.Plugin(pluginapi.LogEntry{Plugin: "p", Level: "info", Msg: "second line"})
	l.Notice("third notice")
	l.Close()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	last := -1
	for _, want := range []string{"first notice", "second line", "third notice"} {
		i := strings.Index(got, want)
		if i < 0 || strings.Count(got, want) != 1 || i < last {
			t.Fatalf("file = %q: %q is missing, repeated or out of order", got, want)
		}
		last = i
	}
}

// A warning that a plugin logs and the status row shows as a notice is written once.
func TestFileLogWritesAnEventOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gvim.log")
	l, err := openLogFile(path)
	if err != nil {
		t.Fatal(err)
	}
	l.Plugin(pluginapi.LogEntry{Plugin: "hello", Level: "warn", Msg: "slow down"})
	l.Notice("hello: slow down")
	l.Close()
	b, _ := os.ReadFile(path)
	if n := strings.Count(string(b), "slow down"); n != 1 {
		t.Fatalf("the event is written %d times, want once: %q", n, b)
	}
}

// A log file that cannot be opened costs the log and nothing else: the nil log accepts every call.
func TestFileLogIsSafeWhenUnwritable(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := openLogFile(filepath.Join(file, "gvim.log"))
	if err == nil {
		t.Fatal("a path under a file opened")
	}
	l.Notice("nothing is written")
	l.Plugin(pluginapi.LogEntry{Plugin: "p", Level: "warn", Msg: "nor this"})
	l.Close()
}

// The plugin log and the notices of the status row both reach the file, each with its source.
func TestLogFileGetsPluginLinesAndNotices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gvim.log")
	l, err := openLogFile(path)
	if err != nil {
		t.Fatal(err)
	}
	l.Plugin(pluginapi.LogEntry{Plugin: "vi", Level: "error", Msg: "boom"})
	l.Notice("Saved a.txt")
	l.Close()
	b, _ := os.ReadFile(path)
	for _, want := range []string{"plugin(vi) error boom", "notice Saved a.txt"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("file lacks %q: %q", want, b)
		}
	}
}

// A --log-file that cannot be written is said on the status row, and the editor runs without it.
func TestUnwritableLogFileIsNoticed(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, notice := startLogFile(filepath.Join(file, "gvim.log"), nil)
	if l != nil || notice == "" {
		t.Fatalf("log = %v, notice = %q: want no log and a notice", l, notice)
	}
}
