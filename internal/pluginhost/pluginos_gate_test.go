package pluginhost

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin/hostos"
)

// The plugin OS trust gate: Run and Start call it before the process is built, with the
// child's effective environment (the parent environment plus the project overlay), and a
// refusal means nothing spawns.

func TestPluginOSGateDeniesBeforeExec(t *testing.T) {
	t.Parallel()
	osys := newPluginOS(nil, func() []string { return []string{"PATH=/proj/bin", "FROM_OVERLAY=1"} })
	var calls int
	var gotEnv map[string]string
	var gotCwd string
	osys.gate = func(_ context.Context, argv []string, cwd string, env map[string]string) error {
		calls++
		gotEnv = env
		gotCwd = cwd
		return errors.New("workspace trust: the workspace is Restricted")
	}
	ctx := context.Background()
	opts := hostos.ProcOptions{Cwd: t.TempDir(), Env: map[string]string{"EXTRA": "2"}}
	if _, err := osys.Run(ctx, []string{"/bin/echo", "hi"}, opts); err == nil || !strings.Contains(err.Error(), "Restricted") {
		t.Fatalf("Run error = %v", err)
	}
	if _, err := osys.Start(ctx, []string{"/bin/echo", "hi"}, opts); err == nil || !strings.Contains(err.Error(), "Restricted") {
		t.Fatalf("Start error = %v", err)
	}
	if calls != 2 {
		t.Errorf("gate calls = %d", calls)
	}
	if gotEnv["PATH"] != "/proj/bin" || gotEnv["FROM_OVERLAY"] != "1" || gotEnv["EXTRA"] != "2" {
		t.Errorf("gate environment = %+v", gotEnv)
	}
	if gotCwd != opts.Cwd {
		t.Errorf("gate cwd = %q, want %q", gotCwd, opts.Cwd)
	}
}

func TestPluginOSGateAllowsThrough(t *testing.T) {
	t.Parallel()
	osys := newPluginOS(nil, nil)
	seen := false
	osys.gate = func(_ context.Context, argv []string, _ string, env map[string]string) error {
		seen = true
		if argv[0] != "/bin/sh" {
			t.Errorf("gate argv = %v", argv)
		}
		if env["PATH"] != "/child/bin" {
			t.Errorf("gate PATH = %q", env["PATH"])
		}
		return nil
	}
	ctx := context.Background()
	if _, err := osys.Run(ctx, []string{"/bin/sh", "-c", "exit 0"}, hostos.ProcOptions{Cwd: t.TempDir(), Env: map[string]string{"PATH": "/child/bin"}}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !seen {
		t.Error("the gate was not called")
	}
}

func TestPluginOSResolvesTheProgramItExecutes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tool := filepath.Join(dir, "childtool")
	marker := filepath.Join(dir, "ran")
	script := "#!/bin/sh\nprintf ran > " + marker + "\n"
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tool, 0o755); err != nil {
		t.Fatal(err)
	}
	osys := newPluginOS(nil, nil)
	osys.gate = func(context.Context, []string, string, map[string]string) error { return nil }
	ctx := context.Background()
	if _, err := osys.Run(ctx, []string{"childtool"}, hostos.ProcOptions{Cwd: t.TempDir(), Env: map[string]string{"PATH": dir}}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the resolved program did not run: %v", err)
	}
	// A bare name the child PATH cannot resolve is refused rather than handed to exec's
	// parent-environment LookPath.
	if _, err := osys.Run(ctx, []string{"definitely-not-a-tool"}, hostos.ProcOptions{Cwd: t.TempDir(), Env: map[string]string{"PATH": dir}}); err == nil {
		t.Fatal("an unresolved bare name was executed")
	}
}
