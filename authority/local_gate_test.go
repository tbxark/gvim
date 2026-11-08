package authority_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/plugin/plugintest"
)

// The local slot's environment-aware trust gate: every launch calls it with the argv,
// the working directory and the environment the child will inherit (so a bare command
// name can be resolved through the child's PATH), and a refusal prevents the launch.

type gateCall struct {
	argv []string
	cwd  string
	env  map[string]string
}

func TestLocalGateRunsBeforeTheLaunch(t *testing.T) {
	t.Parallel()
	fos := plugintest.NewFakeOS()
	fos.SetDir("/w")
	fos.Script([]string{"git", "grep", "x"}, plugintest.ProcScript{})
	overlay := func() []string { return []string{"PATH=/proj/bin", "FROM_OVERLAY=1"} }
	var calls []gateCall
	gate := func(_ context.Context, argv []string, cwd string, env map[string]string) error {
		calls = append(calls, gateCall{argv: argv, cwd: cwd, env: env})
		return nil
	}
	a := authority.Local(authority.LocalOptions{OS: fos, GateEnv: gate, Env: overlay})
	ctx := context.Background()
	if _, err := a.OneShot().Run(ctx, []string{"git", "grep", "x"}, hostos.ProcOptions{Cwd: "/w", Env: map[string]string{"EXTRA": "2", "PATH": "/job/bin"}}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(calls) != 1 || calls[0].argv[0] != "git" || calls[0].cwd != "/w" {
		t.Fatalf("gate calls = %+v", calls)
	}
	env := calls[0].env
	if env["PATH"] != "/proj/bin" || env["FROM_OVERLAY"] != "1" || env["EXTRA"] != "2" {
		t.Errorf("gate environment = %+v", env)
	}
	if env["HOME"] == "" && len(env) < 5 {
		t.Errorf("gate environment lacks the parent environment: %+v", env)
	}
}

func TestLocalGateDeniesRunAndStart(t *testing.T) {
	t.Parallel()
	fos := plugintest.NewFakeOS()
	a := authority.Local(authority.LocalOptions{
		OS: fos,
		GateEnv: func(context.Context, []string, string, map[string]string) error {
			return errors.New("workspace trust: denied")
		},
	})
	ctx := context.Background()
	if _, err := a.OneShot().Run(ctx, []string{"./gradlew"}, hostos.ProcOptions{Cwd: "/w"}); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("Run error = %v", err)
	}
	if _, err := a.LongRunning().Start(ctx, []string{"./gradlew"}, hostos.ProcOptions{Cwd: "/w"}); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("Start error = %v", err)
	}
	if runs := fos.Runs(); len(runs) != 0 {
		t.Errorf("a denied launch reached the OS: %v", runs)
	}
}

func TestLocalTerminalGateSeesTheShell(t *testing.T) {
	t.Parallel()
	fos := plugintest.NewFakeOS()
	fos.SetDir("/w")
	var got []string
	deny := false
	gate := func(_ context.Context, argv []string, cwd string, env map[string]string) error {
		got = append([]string(nil), argv...)
		if deny {
			return errors.New("workspace trust: the workspace is Blocked")
		}
		return nil
	}
	a := authority.Local(authority.LocalOptions{OS: fos, GateEnv: gate, Env: func() []string { return []string{"PATH=/bin"} }})
	ctx := context.Background()
	launch, err := a.Terminal().Launch(ctx, authority.TerminalOptions{Dir: "/w", Shell: "/proj/bin/sh", Args: []string{"-i"}})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if launch.Shell != "/proj/bin/sh" || len(got) != 1 || got[0] != "/proj/bin/sh" {
		t.Fatalf("terminal gate argv = %v, launch = %+v", got, launch)
	}
	deny = true
	if _, err := a.Terminal().Launch(ctx, authority.TerminalOptions{Dir: "/w", Shell: "/proj/bin/sh"}); err == nil {
		t.Fatal("a denied terminal launch was allowed")
	}
}

func TestLocalFallsBackToThePlainGate(t *testing.T) {
	t.Parallel()
	fos := plugintest.NewFakeOS()
	fos.SetDir("/w")
	fos.Script([]string{"echo"}, plugintest.ProcScript{})
	called := 0
	plain := func(_ context.Context, argv []string, cwd string) error {
		called++
		return nil
	}
	a := authority.Local(authority.LocalOptions{OS: fos, Gate: plain})
	ctx := context.Background()
	if _, err := a.OneShot().Run(ctx, []string{"echo"}, hostos.ProcOptions{Cwd: "/w"}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if called != 1 {
		t.Errorf("plain gate calls = %d", called)
	}
}

func TestLocalTerminalShellIsResolved(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("no /bin/sh-style shell to resolve")
	}
	fos := plugintest.NewFakeOS()
	fos.SetDir("/w")
	gate := func(context.Context, []string, string, map[string]string) error { return nil }
	a := authority.Local(authority.LocalOptions{OS: fos, GateEnv: gate})
	launch, err := a.Terminal().Launch(context.Background(), authority.TerminalOptions{
		Dir: "/w", Shell: "sh", Env: []string{"PATH=" + os.Getenv("PATH")},
	})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if !filepath.IsAbs(launch.Shell) {
		t.Errorf("terminal shell = %q, want the resolved absolute program the gate judged", launch.Shell)
	}
}
