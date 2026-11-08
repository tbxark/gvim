package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/config"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/authority/authoritytest"
	"github.com/tbxark/gvim/env"
	"github.com/tbxark/gvim/lsp"
	"github.com/tbxark/gvim/plugin/hostos"
)

// remoteEnvScript is a scripted remote one-shot host for the env provider: it answers the
// marker probe, the baseline and the activation capture, and records every call's
// environment (which must never carry the local overlay).
type remoteEnvScript struct {
	calls []oneShotCall
}

func (s *remoteEnvScript) Run(_ context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
	s.calls = append(s.calls, oneShotCall{argv: append([]string(nil), argv...), cwd: o.Cwd, env: map[string]string{}})
	for k, v := range o.Env {
		s.calls[len(s.calls)-1].env[k] = v
	}
	if len(argv) < 3 || argv[0] != "sh" || argv[1] != "-c" {
		return hostos.ProcResult{}, errors.New("unexpected remote command")
	}
	script := argv[2]
	switch {
	case strings.Contains(script, "set -a;"):
		return hostos.ProcResult{Stdout: []byte("HOME=/home/u\x00PATH=/venv/bin\x00REMOTE_ONLY=1\x00")}, nil
	case strings.HasSuffix(script, "env -0"):
		return hostos.ProcResult{Stdout: []byte("HOME=/home/u\x00PATH=/usr/bin\x00TERM=xterm\x00")}, nil
	default:
		return hostos.ProcResult{Stdout: []byte("0\n")}, nil
	}
}

// TestRemoteEnvRunnerUsesTheRemoteHostOnly: the provider's closure runs sh -c remotely with
// NO environment map (no local overlay, no recursion), the remote cwd, and the output bound.
func TestRemoteEnvRunnerUsesTheRemoteHostOnly(t *testing.T) {
	script := &remoteEnvScript{}
	slot := authoritytest.New()
	slot.OneShotter = script
	root := "/remote/proj"
	run := newRemoteEnvRunner(func() authority.Slot { return slot }, func() string { return root })
	if _, err := run(context.Background(), "cd /remote/proj && env -0"); err != nil {
		t.Fatalf("runner: %v", err)
	}
	if len(script.calls) != 1 {
		t.Fatalf("calls = %+v", script.calls)
	}
	call := script.calls[0]
	if len(call.argv) != 3 || call.argv[0] != "sh" || call.argv[1] != "-c" {
		t.Fatalf("argv = %v", call.argv)
	}
	if call.cwd != root {
		t.Fatalf("cwd = %q", call.cwd)
	}
	if len(call.env) != 0 {
		t.Fatalf("the local environment leaked into the remote command: %v", call.env)
	}
	// No authority mounted yet: a clear error, not a local fallback.
	none := newRemoteEnvRunner(func() authority.Slot { return nil }, func() string { return root })
	if _, err := none(context.Background(), "true"); err == nil || !strings.Contains(err.Error(), "not mounted") {
		t.Fatalf("nil slot = %v", err)
	}
	// A non-zero exit is an error with the remote stderr tail.
	failing := authoritytest.New()
	failing.OneShotter = oneShotFunc(func(context.Context, []string, hostos.ProcOptions) (hostos.ProcResult, error) {
		return hostos.ProcResult{Code: 2, Stderr: []byte("boom\n")}, nil
	})
	run = newRemoteEnvRunner(func() authority.Slot { return failing }, func() string { return root })
	if _, err := run(context.Background(), "true"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("failed command = %v", err)
	}
}

// oneShotFunc adapts a function to authority.OneShot (the authoritytest slot's seam).
type oneShotFunc func(context.Context, []string, hostos.ProcOptions) (hostos.ProcResult, error)

func (f oneShotFunc) Run(ctx context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
	return f(ctx, argv, o)
}

// TestRemoteEnvProviderEndToEnd is the data-safety regression: a remote launch's provider
// detects markers and captures the delta through the fake remote host, and every remote
// command it sent had an empty env map - the local overlay (and its secret) never crossed.
func TestRemoteEnvProviderEndToEnd(t *testing.T) {
	script := &remoteEnvScript{}
	slot := authoritytest.New()
	slot.OneShotter = script
	trusted := true
	provider := env.NewRemote("/remote/proj", configEnvDetector(), func() bool { return trusted }, newRemoteEnvRunner(func() authority.Slot { return slot }, func() string { return "/remote/proj" }))
	provider.Warm(context.Background())
	if det, ok := provider.Detected(); !ok || det.Name != "direnv" {
		t.Fatalf("detected = %+v, %v", det, ok)
	}
	if err := provider.Set("eval \"$(direnv export sh)\"", "", config.EnvKindShell); err != nil {
		t.Fatal(err)
	}
	overlay := provider.Overlay()
	want := []string{"PATH=/venv/bin", "REMOTE_ONLY=1"}
	if len(overlay) != len(want) {
		t.Fatalf("overlay = %v, want %v", overlay, want)
	}
	for i := range want {
		if overlay[i] != want[i] {
			t.Fatalf("overlay = %v", overlay)
		}
	}
	// The trusted gate is live: revoking trust drops the overlay and never re-runs the
	// snippet.
	trusted = false
	if got := provider.Overlay(); len(got) != 0 {
		t.Fatalf("untrusted overlay = %v", got)
	}
	for _, call := range script.calls {
		if len(call.env) != 0 {
			t.Fatalf("a local env entry reached the remote host: %v", call.env)
		}
	}
}

// configEnvDetector is a reducer for the test's literal detector (keeps the import list
// short and the detector shape explicit).
func configEnvDetector() []config.EnvDetector {
	return []config.EnvDetector{{
		Name: "direnv", Markers: []string{".envrc"}, Kind: config.EnvKindShell,
		Snippet: "eval \"$(direnv export sh)\"",
	}}
}

// TestLocalEnvOverlayIsLocalOnly: the helper that gates the plugin host and the local
// authority returns the overlay for a local launch and nil for a remote one.
func TestLocalEnvOverlayIsLocalOnly(t *testing.T) {
	provider := env.New(t.TempDir(), configEnvDetector(), func() bool { return true })
	if localEnvOverlay(provider, true) != nil {
		t.Fatal("a remote launch got a local env overlay")
	}
	if fn := localEnvOverlay(provider, false); fn == nil {
		t.Fatal("a local launch lost its env overlay")
	}
	if localEnvOverlay(nil, false) != nil {
		t.Fatal("a nil provider produced an overlay")
	}
}

// TestRemoteLSPSpawnerPassesTheRemoteEnv: the remote language server starts with the
// session's captured remote delta as its ProcOptions env (the authority applies it on the
// remote host), never the local overlay.
func TestRemoteLSPSpawnerPassesTheRemoteEnv(t *testing.T) {
	proc := newFakeLSPProc()
	runner := &fakeLongRunner{proc: proc}
	slot := authoritytest.New()
	slot.LongRunner = runner
	spawn := remoteLSPSpawner(slot, "/remote/proj", func() []string {
		return []string{"PATH=/venv/bin", "REMOTE_ONLY=1"}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := spawn(ctx, lsp.Config{Command: []string{"gopls", "serve"}, RootURI: "file:///local/proj"})
	if err != nil {
		t.Fatalf("spawner: %v", err)
	}
	defer func() { _ = client.Close() }()
	if runner.opts.Cwd != "/remote/proj" {
		t.Fatalf("cwd = %q", runner.opts.Cwd)
	}
	if runner.opts.Env["PATH"] != "/venv/bin" || runner.opts.Env["REMOTE_ONLY"] != "1" {
		t.Fatalf("remote LSP env = %v", runner.opts.Env)
	}
}
