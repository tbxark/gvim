package env

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/trust"
)

// fakeRunner answers the two capture shells with fixed environments.
func fakeRunner(base, active string) (Runner, *int) {
	calls := 0
	return func(dir, command string) (string, error) {
		calls++
		if strings.HasPrefix(command, "set -a; ") {
			return active, nil
		}
		return base, nil
	}, &calls
}

func TestProviderDetectionIsPassive(t *testing.T) {
	root := t.TempDir()
	p := New(root, nil, func() bool { return true })
	if _, ok := p.Detected(); ok {
		t.Error("detected an environment in an empty workspace")
	}
	if err := os.MkdirAll(filepath.Join(root, ".venv/bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".venv/bin/python"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	p = New(root, nil, func() bool { return true })
	det, ok := p.Detected()
	if !ok || det.Name != ".venv" {
		t.Fatalf("detected = %+v, %v; want .venv", det, ok)
	}
}

func TestProviderTrustGate(t *testing.T) {
	root := t.TempDir()
	trusted := false
	p := New(root, nil, func() bool { return trusted })
	if err := p.Set("export A=1", root, config.EnvKindShell); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("untrusted Set error = %v, want ErrUntrusted", err)
	}
	trusted = true
	if err := p.Set("export A=1", root, config.EnvKindShell); err != nil {
		t.Fatalf("trusted Set error = %v", err)
	}
	if info, ok := p.Active(); !ok || info.Kind != config.EnvKindShell {
		t.Errorf("active = %+v, %v", info, ok)
	}
	// Losing trust stops applying the overlay.
	trusted = false
	p.SetRunner(func(dir, command string) (string, error) { return "", nil })
	if got := p.Overlay(); got != nil {
		t.Errorf("overlay while untrusted = %v, want nil", got)
	}
}

func TestProviderCaptureDeltaAndOverlay(t *testing.T) {
	root := t.TempDir()
	base := "A=1\x00B=2\x00X=9\x00PWD=/w\x00"
	active := "A=1\x00B=3\x00C=4\x00PWD=/w\x00"
	runner, calls := fakeRunner(base, active)
	p := New(root, nil, func() bool { return true })
	p.SetRunner(runner)
	if err := p.Set("export C=4", root, config.EnvKindShell); err != nil {
		t.Fatal(err)
	}
	overlay := p.Overlay()
	if len(overlay) != 2 || overlay[0] != "B=3" || overlay[1] != "C=4" {
		t.Fatalf("overlay = %v, want [B=3 C=4]", overlay)
	}
	if *calls != 2 {
		t.Errorf("capture ran %d shells, want the baseline and the activation", *calls)
	}
	if got := p.Overlay(); len(got) != 2 || *calls != 2 {
		t.Errorf("second Overlay = %v (%d calls), want the cache", got, *calls)
	}
	if unset := p.Unset(); len(unset) != 1 || unset[0] != "X" {
		t.Errorf("unset = %v, want [X]", unset)
	}
}

func TestProviderCaptureFailureDegrades(t *testing.T) {
	root := t.TempDir()
	p := New(root, nil, func() bool { return true })
	p.SetRunner(func(dir, command string) (string, error) {
		return "", errors.New("direnv exploded")
	})
	if err := p.Set("direnv export bash", root, config.EnvKindShell); err != nil {
		t.Fatal(err)
	}
	if got := p.Overlay(); got != nil {
		t.Fatalf("overlay = %v, want nil on a failed capture", got)
	}
	if p.LastError() == nil {
		t.Error("LastError = nil, want the capture failure")
	}
}

func TestProviderRefreshOnInputChange(t *testing.T) {
	root := t.TempDir()
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(root, ".envrc"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("one")
	runner, calls := fakeRunner("A=1", "A=2")
	p := New(root, nil, func() bool { return true })
	p.SetRunner(runner)
	if err := p.Set("direnv export bash", root, config.EnvKindShell); err != nil {
		t.Fatal(err)
	}
	p.Overlay()
	write("two-changed-size")
	p.Overlay()
	if *calls != 4 {
		t.Errorf("capture ran %d shells, want a re-capture after .envrc changed", *calls)
	}
}

var _ = trust.DetectedEnv{}

// TestProviderCaptureDoesNotBlockReaders checks the status readers answer while a slow
// activation shell runs, and a second spawn waits for the same capture instead of
// running the shells again.
func TestProviderCaptureDoesNotBlockReaders(t *testing.T) {
	root := t.TempDir()
	release, entered := make(chan struct{}), make(chan struct{}, 4)
	var mu sync.Mutex
	calls := 0
	p := New(root, nil, func() bool { return true })
	p.SetRunner(func(_, command string) (string, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		entered <- struct{}{}
		<-release
		if strings.Contains(command, "set -a") {
			return "C=4\x00", nil
		}
		return "", nil
	})
	if err := p.Set("export C=4", root, config.EnvKindShell); err != nil {
		t.Fatal(err)
	}
	results := make(chan []string, 2)
	go func() { results <- p.Overlay() }()
	<-entered // the capture is running
	go func() { results <- p.Overlay() }()
	read := make(chan struct{})
	go func() {
		_ = p.Info()
		_ = p.LastError()
		close(read)
	}()
	select {
	case <-read:
	case <-time.After(5 * time.Second):
		t.Fatal("the readers waited for the activation shell")
	}
	close(release)
	for range 2 {
		if got := <-results; len(got) != 1 || got[0] != "C=4" {
			t.Fatalf("overlay = %v, want [C=4]", got)
		}
	}
	if calls != 2 {
		t.Fatalf("shells run = %d, want one capture (baseline and activation)", calls)
	}
}
