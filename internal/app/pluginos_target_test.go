package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/plugin"
	"github.com/tbxark/gvim/internal/pluginapi"
)

// An atomic write that fails names the file the plugin asked for, not the ".gvim-*.tmp" file it
// wrote on the way.
func TestAtomicWriteErrorNamesTheTarget(t *testing.T) {
	o, dir := newTestOS(t)
	target := filepath.Join(dir, "out", "sub", "file.txt")
	err := o.WriteFile(context.Background(), target, []byte("hi"), plugin.WriteOptions{Atomic: true})
	if err == nil {
		t.Fatal("no error for a missing directory")
	}
	if !strings.Contains(err.Error(), target) || strings.Contains(err.Error(), ".gvim-") || strings.Contains(err.Error(), ".tmp") {
		t.Fatalf("error = %q, want it to name %s and not the temporary file", err, target)
	}
	if e, ok := err.(*pluginapi.Error); !ok || e.Code != pluginapi.CodeNotFound {
		t.Fatalf("error = %#v, want not_found", err)
	}
}

// A failed atomic write names the file the plugin asked for, not the temporary file next to it.
func TestPluginOSWriteErrorNamesTheTarget(t *testing.T) {
	t.Parallel()
	o, dir := newTestOS(t)
	target := filepath.Join(dir, "out", "sub", "file.txt") // out/sub does not exist
	err := o.WriteFile(context.Background(), target, []byte("hi"), plugin.WriteOptions{Atomic: true})
	if err == nil {
		t.Fatal("writing into a missing directory succeeded")
	}
	if got := pluginapi.CodeOf(err); got != pluginapi.CodeNotFound {
		t.Errorf("code = %q, want not_found", got)
	}
	if !strings.Contains(err.Error(), target) {
		t.Errorf("error %q does not name the target %q", err, target)
	}
	if strings.Contains(err.Error(), ".gvim-") || strings.Contains(err.Error(), ".tmp") {
		t.Errorf("error %q names the temporary file", err)
	}
}
