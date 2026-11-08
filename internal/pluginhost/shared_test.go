package pluginhost

import (
	"context"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/plugin/plugintest"
)

// TestMuxOSFollowsTheActiveWorkspace: the shared runtime's OS delegates to the active
// workspace's OS, and Shutdown ends every registered workspace OS.
func TestMuxOSFollowsTheActiveWorkspace(t *testing.T) {
	a := plugintest.NewFakeOS()
	b := plugintest.NewFakeOS()
	a.SetFile("/a/one.txt", "A")
	b.SetFile("/b/two.txt", "B")
	mux := &MuxOS{}
	mux.Register(a)
	mux.Register(b)
	mux.SetActive(a)
	if data, err := mux.ReadFile(context.Background(), "/a/one.txt", 1<<20); err != nil || string(data) != "A" {
		t.Fatalf("active A read = %q, %v", data, err)
	}
	if _, err := mux.ReadFile(context.Background(), "/b/two.txt", 1<<20); err == nil {
		t.Fatal("the inactive workspace's file was readable through the active OS")
	}
	mux.SetActive(b)
	if data, err := mux.ReadFile(context.Background(), "/b/two.txt", 1<<20); err != nil || string(data) != "B" {
		t.Fatalf("active B read = %q, %v", data, err)
	}
	dirs := mux.Dirs()
	if dirs.Config != b.Dirs().Config {
		t.Fatalf("mux Dirs = %+v, want the active OS's %+v", dirs, b.Dirs())
	}
	mux.Shutdown()
	if !a.ShutdownCalled() || !b.ShutdownCalled() {
		t.Fatalf("Shutdown did not end both workspace OSes (a=%v b=%v)", a.ShutdownCalled(), b.ShutdownCalled())
	}
}

// TestDaemonEnsureBuildsOneRuntime: the daemon's shared runtime is built by the first
// workspace and reused by every later one (no runtime growth on a switch).
func TestDaemonEnsureBuildsOneRuntime(t *testing.T) {
	d := NewDaemon()
	cfg := config.Config{}
	o := Options{Version: "test", Root: func() string { return "/w1" }}
	rt1, keys1 := d.Ensure(cfg, o)
	if rt1 == nil || keys1 == nil {
		t.Fatal("Ensure built nothing")
	}
	rt2, keys2 := d.Ensure(cfg, o)
	if rt2 != rt1 || d.Runtime() != rt1 {
		t.Fatal("Ensure built a second runtime")
	}
	if keys2 == nil {
		t.Fatal("Ensure returned no keymap builder")
	}
	d.Stop()
}
