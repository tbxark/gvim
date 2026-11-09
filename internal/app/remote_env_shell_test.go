package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/authority/authoritytest"
	"github.com/tbxark/gvim/plugin/hostos"
)

// shellScript answers the shell probe and the interactive/sh capture commands.
type shellScript struct {
	mu        sync.Mutex
	calls     []oneShotCall
	probe     string
	failShell string // when set, the interactive <shell> -ilc call fails
}

func (s *shellScript) Run(_ context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	call := oneShotCall{argv: append([]string(nil), argv...), cwd: o.Cwd, env: map[string]string{}}
	for k, v := range o.Env {
		call.env[k] = v
	}
	s.calls = append(s.calls, call)
	switch {
	case len(argv) == 3 && argv[0] == "sh" && argv[1] == "-c" && argv[2] == remoteEnvShellProbe:
		return hostos.ProcResult{Stdout: []byte(s.probe)}, nil
	case len(argv) == 3 && (argv[0] == "bash" || argv[0] == "zsh") && argv[1] == "-ilc":
		if s.failShell == argv[0] {
			return hostos.ProcResult{Code: 1, Stderr: []byte(argv[0] + ": rc init failed")}, nil
		}
		if !strings.Contains(argv[2], "GVIM_ENV_OUT") {
			return hostos.ProcResult{}, errors.New("interactive command lacks the marker")
		}
		return hostos.ProcResult{Stdout: []byte("startup noise\n" + remoteEnvShellMarker + "HOME=/home/u\x00PATH=/venv/bin\x00")}, nil
	case len(argv) == 3 && argv[0] == "sh" && argv[1] == "-c":
		return hostos.ProcResult{Stdout: []byte("HOME=/home/u\x00PATH=/usr/bin\x00")}, nil
	}
	return hostos.ProcResult{}, errors.New("unexpected remote command")
}

func newShellEnvRunner(t *testing.T, script *shellScript, notices *[]string) *remoteEnvShell {
	t.Helper()
	slot := authoritytest.New()
	slot.OneShotter = script
	return newRemoteEnvShell(func() authority.Slot { return slot }, func() string { return "/remote/proj" }, func(msg string) {
		if notices != nil {
			*notices = append(*notices, msg)
		}
	})
}

func TestRemoteEnvShellCaptureUsesInteractiveLoginShell(t *testing.T) {
	script := &shellScript{probe: "bash"}
	var notices []string
	runner := newShellEnvRunner(t, script, &notices)
	runner.Prepare(context.Background())
	if got := runner.Notice(); got != "" {
		t.Fatalf("unexpected notice = %q", got)
	}
	out, err := runner.RunCapture(context.Background(), "cd /remote/proj && env -0")
	if err != nil {
		t.Fatal(err)
	}
	if out != "HOME=/home/u\x00PATH=/venv/bin\x00" {
		t.Fatalf("interactive capture output = %q (rc noise must be stripped)", out)
	}
	script.mu.Lock()
	calls := append([]oneShotCall(nil), script.calls...)
	script.mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("calls = %+v", calls)
	}
	if calls[0].argv[0] != "sh" || calls[0].argv[2] != remoteEnvShellProbe {
		t.Fatalf("probe = %v, want sh -c <probe>", calls[0].argv)
	}
	if calls[1].argv[0] != "bash" || calls[1].argv[1] != "-ilc" || !strings.Contains(calls[1].argv[2], "printf '\000GVIM_ENV_OUT\000'; cd /remote/proj && env -0") {
		t.Fatalf("capture argv = %v, want bash -ilc with the marker prefix", calls[1].argv)
	}
	if len(calls[1].env) != 0 {
		t.Fatalf("the local environment leaked into the interactive capture: %v", calls[1].env)
	}
}

func TestRemoteEnvShellNoInteractiveFallsBackWithNotice(t *testing.T) {
	script := &shellScript{probe: "sh"}
	var notices []string
	runner := newShellEnvRunner(t, script, &notices)
	runner.Prepare(context.Background())
	if got := runner.Notice(); !strings.Contains(got, "no interactive bash/zsh") || !strings.Contains(got, "using sh -c") {
		t.Fatalf("fallback notice = %q", got)
	}
	out, err := runner.RunCapture(context.Background(), "cd /remote/proj && env -0")
	if err != nil || !strings.Contains(out, "PATH=/usr/bin") {
		t.Fatalf("sh fallback capture = %q, %v", out, err)
	}
	script.mu.Lock()
	defer script.mu.Unlock()
	last := script.calls[len(script.calls)-1]
	if last.argv[0] != "sh" || last.argv[1] != "-c" || last.argv[2] != "cd /remote/proj && env -0" {
		t.Fatalf("fallback argv = %v, want the previous sh -c command", last.argv)
	}
	if len(notices) == 0 || !strings.Contains(notices[0], "using sh -c") {
		t.Fatalf("notice callback = %v", notices)
	}
}

func TestRemoteEnvShellInteractiveFailureFallsBack(t *testing.T) {
	script := &shellScript{probe: "zsh", failShell: "zsh"}
	var notices []string
	runner := newShellEnvRunner(t, script, &notices)
	out, err := runner.RunCapture(context.Background(), "cd /remote/proj && env -0")
	if err != nil || !strings.Contains(out, "PATH=/usr/bin") {
		t.Fatalf("failure fallback = %q, %v", out, err)
	}
	got := runner.Notice()
	if !strings.Contains(got, "zsh -ilc failed") || !strings.Contains(got, "using sh -c for this capture") {
		t.Fatalf("failure notice = %q", got)
	}
	if len(notices) == 0 || !strings.Contains(notices[0], "zsh -ilc failed") {
		t.Fatalf("notice callback = %v", notices)
	}
	script.mu.Lock()
	defer script.mu.Unlock()
	if len(script.calls) != 3 || script.calls[1].argv[0] != "zsh" || script.calls[2].argv[0] != "sh" {
		t.Fatalf("calls = %+v, want probe, zsh, sh", script.calls)
	}
}

func TestRemoteEnvShellDetectionStaysOnSh(t *testing.T) {
	script := &shellScript{probe: "bash"}
	runner := newShellEnvRunner(t, script, nil)
	runner.Prepare(context.Background())
	if _, err := runner.RunDetect(context.Background(), "cd /remote/proj && true"); err != nil {
		t.Fatal(err)
	}
	script.mu.Lock()
	defer script.mu.Unlock()
	last := script.calls[len(script.calls)-1]
	if last.argv[0] != "sh" || last.argv[1] != "-c" {
		t.Fatalf("detection argv = %v, want plain sh -c even with bash available", last.argv)
	}
}
