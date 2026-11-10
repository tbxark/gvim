package pluginhost

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/pluginapi"
)

// TestPluginOSCopyRefusesExistingFile: local fs.copy no longer opens the destination
// with O_TRUNC after a check; an existing file is a conflict and its bytes stay.
func TestPluginOSCopyRefusesExistingFile(t *testing.T) {
	o, dir := newTestOS(t)
	writePluginTree(t, dir, map[string]string{"a.txt": "A", "b.txt": "B"})
	err := o.Copy(context.Background(), "a.txt", "b.txt")
	if err == nil || codeOf(err) != pluginapi.CodeConflict {
		t.Fatalf("copy onto existing file = %v, code %q", err, codeOf(err))
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "b.txt")); string(b) != "B" {
		t.Fatalf("existing destination changed to %q", b)
	}
}

// TestPluginOSCopyConcurrentDestinationIsExclusive: fs.copy publishes the target through
// the same atomic no-replace path; one concurrent copy wins and the other conflicts.
func TestPluginOSCopyConcurrentDestinationIsExclusive(t *testing.T) {
	o, dir := newTestOS(t)
	writePluginTree(t, dir, map[string]string{"a.txt": "payload"})
	errs := make(chan error, 2)
	for range 2 {
		go func() { errs <- o.Copy(context.Background(), "a.txt", "b.txt") }()
	}
	var wins, conflicts int
	for range 2 {
		err := <-errs
		switch {
		case err == nil:
			wins++
		case codeOf(err) == pluginapi.CodeConflict:
			conflicts++
		default:
			t.Fatalf("concurrent fs.copy error = %v, code %q", err, codeOf(err))
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("concurrent fs.copy wins=%d conflicts=%d, want 1/1", wins, conflicts)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "b.txt")); string(b) != "payload" {
		t.Fatalf("destination content = %q", b)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".fsops-copy-") {
			t.Fatalf("copy left a temporary file: %s", e.Name())
		}
	}
}

// TestPluginOSCopyTreeConcurrentDestinationIsExclusive covers the tree claim.
func TestPluginOSCopyTreeConcurrentDestinationIsExclusive(t *testing.T) {
	o, dir := newTestOS(t)
	writePluginTree(t, dir, map[string]string{"src/x.txt": "X"})
	errs := make(chan error, 2)
	for range 2 {
		go func() { errs <- o.Copy(context.Background(), "src", "dst") }()
	}
	var wins, conflicts int
	for range 2 {
		err := <-errs
		switch {
		case err == nil:
			wins++
		case codeOf(err) == pluginapi.CodeConflict:
			conflicts++
		default:
			t.Fatalf("concurrent tree fs.copy error = %v, code %q", err, codeOf(err))
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("concurrent tree fs.copy wins=%d conflicts=%d, want 1/1", wins, conflicts)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "dst", "x.txt")); string(b) != "X" {
		t.Fatalf("tree destination content = %q", b)
	}
}

// TestPluginOSCopyCancellation: fs.copy returns the plugin cancellation code and leaves
// no destination; the local path passes the plugin context into the atomic fsops copy.
func TestPluginOSCopyCancellation(t *testing.T) {
	o, dir := newTestOS(t)
	writePluginTree(t, dir, map[string]string{"a.txt": strings.Repeat("x", 64<<10)})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := o.Copy(ctx, "a.txt", "b.txt")
	if err == nil || codeOf(err) != pluginapi.CodeCancelled {
		t.Fatalf("cancelled copy = %v, code %q", err, codeOf(err))
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); !os.IsNotExist(err) {
		t.Fatal("cancelled copy left a destination")
	}
}

// TestPluginOSCopyWarningReachesLogSink: the fsops hard-link fallback warning is not
// silently discarded; pluginOS surfaces it through the plugin warning log.
func TestPluginOSCopyWarningReachesLogSink(t *testing.T) {
	o, _ := newTestOS(t)
	var entries []pluginapi.LogEntry
	o.logSink = func(e pluginapi.LogEntry) { entries = append(entries, e) }
	o.warnCopy("file copy used the O_EXCL fallback; a failed write can leave a partial target")
	if len(entries) != 1 || entries[0].Level != "warn" || !strings.Contains(entries[0].Msg, "O_EXCL fallback") {
		t.Fatalf("warning log = %+v", entries)
	}
}
