//go:build unix

package plugin_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestDiscoveryOverARealNamedPipe runs the default (os.OpenRoot and os.DirFS) readers over
// real FIFOs: discovery must finish and the other plugins must load.
func TestDiscoveryOverARealNamedPipe(t *testing.T) {
	dir := t.TempDir()
	plugins := filepath.Join(dir, "plugins")
	for _, d := range []string{"", "dirplug"} {
		if err := os.MkdirAll(filepath.Join(plugins, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(plugins, "ok.lua"), []byte(marker("ok")), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(plugins, "pipe.lua"), filepath.Join(plugins, "dirplug", "init.lua"), filepath.Join(dir, "init.lua")} {
		if err := syscall.Mkfifo(p, 0o644); err != nil {
			t.Skipf("mkfifo: %v", err)
		}
	}
	e := newEnv(t)
	e.opts.Open = nil
	e.opts.UserDir = plugins
	e.opts.InitFile = filepath.Join(dir, "init.lua")
	e.start()
	if p := e.info("ok"); p.Status != "loaded" {
		t.Errorf("ok = %+v", p)
	}
	for _, name := range []string{"pipe", "dirplug", "init"} {
		if p := e.info(name); p.Status != "failed" {
			t.Errorf("%s = %+v, want failed", name, p)
		}
	}
}
