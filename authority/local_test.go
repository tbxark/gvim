package authority_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/plugin/plugintest"
)

// TestLocalForwardsFiles checks the local slot is byte-identical to the OS port: the same
// data and the same errors.
func TestLocalForwardsFiles(t *testing.T) {
	fos := plugintest.NewFakeOS()
	fos.SetFile("/w/a.txt", "hello")
	fos.SetDir("/w/sub")
	a := authority.Local(authority.LocalOptions{OS: fos})
	ctx := context.Background()

	got, err := a.Files().ReadFile(ctx, "/w/a.txt", 1024)
	want, werr := hostos.OS(fos).ReadFile(ctx, "/w/a.txt", 1024)
	if string(got) != string(want) || err != nil || werr != nil {
		t.Fatalf("ReadFile = %q, %v; port = %q, %v", got, err, want, werr)
	}
	st, err := a.Files().Stat(ctx, "/w/a.txt")
	pst, perr := hostos.OS(fos).Stat(ctx, "/w/a.txt")
	if err != nil || perr != nil || st != pst {
		t.Fatalf("Stat = %+v, %v; port = %+v, %v", st, err, pst, perr)
	}
	entries, err := a.Files().ReadDir(ctx, "/w", false)
	pentries, perr := hostos.OS(fos).ReadDir(ctx, "/w", false)
	if err != nil || perr != nil || len(entries) != len(pentries) {
		t.Fatalf("ReadDir = %v, %v; port = %v, %v", entries, err, pentries, perr)
	}
	if err := a.Files().WriteFile(ctx, "/w/b.txt", []byte("x"), hostos.WriteOptions{}); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got, ok := fos.File("/w/b.txt"); !ok || got != "x" {
		t.Fatalf("written file = %q, %v", got, ok)
	}
	// A missing file gives the port's not-found error through the slot unchanged.
	if _, err := a.Files().ReadFile(ctx, "/w/missing", 10); err == nil {
		t.Fatal("a missing file was read")
	}
}

// TestLocalWatch checks the watch surface reaches the port's watcher.
func TestLocalWatch(t *testing.T) {
	fos := plugintest.NewFakeOS()
	fos.SetDir("/w")
	a := authority.Local(authority.LocalOptions{OS: fos})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w, err := a.Files().Watch(ctx, "/w", hostos.WatchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	fos.Emit("/w/a.txt", "write")
	nextCtx, nextCancel := context.WithTimeout(ctx, time.Second)
	defer nextCancel()
	ev, ok := w.Next(nextCtx)
	if !ok || ev.Path != "/w/a.txt" || ev.Kind != "write" {
		t.Fatalf("watch event = %+v, %v", ev, ok)
	}
}

// TestLocalGateAndEnv checks the choke point: the trust gate runs before every launch and
// the env provider is merged into the process options and the terminal environment.
func TestLocalGateAndEnv(t *testing.T) {
	fos := plugintest.NewFakeOS()
	fos.Script([]string{"git"}, plugintest.ProcScript{Stdout: "ok"})
	var gateArgv []string
	var gateCwd string
	denied := errors.New("trust: restricted")
	gated := authority.Local(authority.LocalOptions{
		OS: fos,
		Gate: func(_ context.Context, argv []string, cwd string) error {
			gateArgv, gateCwd = argv, cwd
			return denied
		},
	})
	if _, err := gated.OneShot().Run(context.Background(), []string{"git", "status"}, hostos.ProcOptions{Cwd: "/w"}); !errors.Is(err, denied) {
		t.Fatalf("gated Run err = %v", err)
	}
	if len(gateArgv) != 2 || gateCwd != "/w" {
		t.Fatalf("gate saw argv %v cwd %q", gateArgv, gateCwd)
	}
	if _, err := gated.LongRunning().Start(context.Background(), []string{"lsp"}, hostos.ProcOptions{}); !errors.Is(err, denied) {
		t.Fatalf("gated Start err = %v", err)
	}
	if _, err := gated.Terminal().Launch(context.Background(), authority.TerminalOptions{Dir: "/w"}); !errors.Is(err, denied) {
		t.Fatalf("gated terminal err = %v", err)
	}

	allowed := authority.Local(authority.LocalOptions{
		OS:  fos,
		Env: func() []string { return []string{"GIT_DIR=/remote/.git"} },
	})
	res, err := allowed.OneShot().Run(context.Background(), []string{"git", "status"}, hostos.ProcOptions{})
	if err != nil || res.Code != 0 || string(res.Stdout) != "ok" {
		t.Fatalf("Run = %+v, %v", res, err)
	}
	runs := fos.Runs()
	if len(runs) != 1 || runs[0].Opts.Env["GIT_DIR"] != "/remote/.git" {
		t.Fatalf("env not merged: %+v", runs)
	}
	launch, err := allowed.Terminal().Launch(context.Background(), authority.TerminalOptions{Dir: "/w"})
	if err != nil {
		t.Fatal(err)
	}
	if launch.Backend != nil || launch.ManagesCwd || len(launch.Env) != 1 || launch.Env[0] != "GIT_DIR=/remote/.git" {
		t.Fatalf("local launch = %+v", launch)
	}
}

// TestLocalTerminalShellProbe checks the configured shell probe is used by the local
// wrapper (the app keeps its own default when Shell is empty).
func TestLocalTerminalShellProbe(t *testing.T) {
	a := authority.Local(authority.LocalOptions{
		OS:    plugintest.NewFakeOS(),
		Shell: func() (string, []string) { return "/bin/zsh", []string{"-l"} },
	})
	launch, err := a.Terminal().Launch(context.Background(), authority.TerminalOptions{Dir: "/w"})
	if err != nil {
		t.Fatal(err)
	}
	if launch.Shell != "/bin/zsh" || len(launch.Args) != 1 || launch.Args[0] != "-l" {
		t.Fatalf("launch = %+v", launch)
	}
}
