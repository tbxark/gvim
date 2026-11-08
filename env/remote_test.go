package env

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/tbxark/gvim/config"
)

// remoteScript is a scripted remote host: it answers the marker probe, the baseline and
// the activation capture from fixed strings and records every command.
type remoteScript struct {
	mu       sync.Mutex
	calls    []string
	markers  string
	baseline string
	active   string
	actErr   error
}

func (s *remoteScript) run(_ context.Context, command string) (string, error) {
	s.mu.Lock()
	s.calls = append(s.calls, command)
	s.mu.Unlock()
	switch {
	case strings.Contains(command, "set -a;"):
		return s.active, s.actErr
	case strings.HasSuffix(command, "env -0"):
		return s.baseline, nil
	default:
		return s.markers, nil
	}
}

func (s *remoteScript) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func direnvDetectors() []config.EnvDetector {
	return []config.EnvDetector{{
		Name:    "direnv",
		Markers: []string{".envrc"},
		Kind:    config.EnvKindShell,
		Snippet: "eval \"$(direnv export sh)\"",
	}}
}

// TestRemoteDetectsOnTheRemoteHost: detection is the remote marker probe (not a local
// scan), the snippet carries the remote root, and Warm only needs that one probe.
func TestRemoteDetectsOnTheRemoteHost(t *testing.T) {
	s := &remoteScript{markers: "0\n", baseline: "HOME=/home/u\x00", active: "HOME=/home/u\x00"}
	p := NewRemote("/remote/proj", direnvDetectors(), func() bool { return true }, s.run)
	p.Warm(context.Background())
	det, ok := p.Detected()
	if !ok || det.Name != "direnv" {
		t.Fatalf("detected = %+v, %v", det, ok)
	}
	if det.Snippet != "eval \"$(direnv export sh)\"" || det.Kind != config.EnvKindShell {
		t.Fatalf("detected environment = %+v", det)
	}
	calls := s.recorded()
	if len(calls) != 1 {
		t.Fatalf("remote calls = %v, want one marker probe", calls)
	}
	if !strings.Contains(calls[0], "cd '/remote/proj'") || !strings.Contains(calls[0], "[ -e '.envrc' ]") {
		t.Fatalf("marker probe = %q", calls[0])
	}
	if !strings.Contains(calls[0], "printf '%s\\n' 0") {
		t.Fatalf("marker probe does not report the matched index: %q", calls[0])
	}
}

// TestRemoteActivationIsTrustGated: an untrusted workspace cannot activate (the remote
// snippet never runs), and the overlay stays empty.
func TestRemoteActivationIsTrustGated(t *testing.T) {
	s := &remoteScript{markers: "0\n"}
	p := NewRemote("/remote/proj", direnvDetectors(), func() bool { return false }, s.run)
	if err := p.Set("eval \"$(direnv export sh)\"", "", config.EnvKindShell); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("Set on an untrusted workspace = %v, want ErrUntrusted", err)
	}
	if got := p.Overlay(); len(got) != 0 {
		t.Fatalf("untrusted overlay = %v", got)
	}
	for _, c := range s.recorded() {
		if strings.Contains(c, "direnv") {
			t.Fatalf("the activation snippet ran on an untrusted workspace: %q", c)
		}
	}
}

// TestRemoteOverlayIsTheRemoteDelta: the overlay is the diff of the REMOTE baseline and
// the REMOTE activated environment (a changed entry, a new entry, a removed entry), never
// anything from the local process environment.
func TestRemoteOverlayIsTheRemoteDelta(t *testing.T) {
	s := &remoteScript{
		markers:  "0\n",
		baseline: "HOME=/home/u\x00PATH=/usr/bin\x00TERM=xterm\x00LOCAL_SECRET=leak\x00",
		active:   "HOME=/home/u\x00PATH=/venv/bin:/usr/bin\x00REMOTE_ONLY=1\x00LOCAL_SECRET=leak\x00",
	}
	p := NewRemote("/remote/proj", direnvDetectors(), func() bool { return true }, s.run)
	if err := p.Set("eval \"$(direnv export sh)\"", "", config.EnvKindShell); err != nil {
		t.Fatal(err)
	}
	overlay := p.Overlay()
	want := []string{"PATH=/venv/bin:/usr/bin", "REMOTE_ONLY=1"}
	if len(overlay) != len(want) {
		t.Fatalf("overlay = %v, want %v", overlay, want)
	}
	for i := range want {
		if overlay[i] != want[i] {
			t.Fatalf("overlay = %v, want %v", overlay, want)
		}
	}
	if got := p.Unset(); len(got) != 1 || got[0] != "TERM" {
		t.Fatalf("unset = %v, want [TERM]", got)
	}
	if info, ok := p.Active(); !ok || info.Name != "direnv" || info.Dir != "/remote/proj" {
		t.Fatalf("active = %+v, %v", info, ok)
	}
	calls := s.recorded()
	var sawBaseline, sawActivation bool
	for _, c := range calls {
		if strings.Contains(c, "cd '/remote/proj' && env -0") {
			sawBaseline = true
		}
		if strings.Contains(c, "set -a; eval \"$(direnv export sh)\"") {
			sawActivation = true
		}
	}
	if !sawBaseline || !sawActivation {
		t.Fatalf("remote calls = %v, want baseline and activation", calls)
	}
}

// TestRemoteQuotesRootAndMarkers: the fixed shell syntax is composed around quoted words,
// so a root or marker name with spaces/quotes cannot inject shell code.
func TestRemoteQuotesRootAndMarkers(t *testing.T) {
	s := &remoteScript{markers: ""}
	root := "/remote/it's proj"
	p := NewRemote(root, []config.EnvDetector{{
		Name: "custom", Markers: []string{"marker with space", "it's.marker"},
		Kind: config.EnvKindShell, Snippet: "true",
	}}, func() bool { return true }, s.run)
	p.Warm(context.Background())
	calls := s.recorded()
	if len(calls) != 1 {
		t.Fatalf("calls = %v", calls)
	}
	probe := calls[0]
	if !strings.Contains(probe, "cd '/remote/it'\"'\"'s proj'") {
		t.Fatalf("root is not single-quote-escaped: %q", probe)
	}
	if !strings.Contains(probe, "[ -e 'marker with space' ]") || !strings.Contains(probe, "[ -e 'it'\"'\"'s.marker' ]") {
		t.Fatalf("markers are not single-quote-escaped: %q", probe)
	}
}

// TestRemoteDetectionFailureIsQuiet: a failed probe leaves detection empty and the overlay
// nil; it never panics or falls back to the local environment.
func TestRemoteDetectionFailureIsQuiet(t *testing.T) {
	run := func(context.Context, string) (string, error) {
		return "", errors.New("ssh: connection refused")
	}
	p := NewRemote("/remote/proj", direnvDetectors(), func() bool { return true }, run)
	p.Warm(context.Background())
	if det, ok := p.Detected(); ok || det.Name != "" {
		t.Fatalf("detected after a failed probe = %+v, %v", det, ok)
	}
	if err := p.Set("true", "", config.EnvKindShell); err != nil {
		t.Fatal(err)
	}
	if got := p.Overlay(); len(got) != 0 {
		t.Fatalf("overlay after a failed capture = %v", got)
	}
	if p.LastError() == nil {
		t.Fatal("the failed capture was not recorded")
	}
}

// TestRemoteCaptureFailureClearsTheOverlay: a capture error must not leave a stale delta
// from a previous recipe.
func TestRemoteCaptureFailureClearsTheOverlay(t *testing.T) {
	s := &remoteScript{markers: "0\n", baseline: "A=1\x00", active: "A=2\x00"}
	p := NewRemote("/remote/proj", direnvDetectors(), func() bool { return true }, s.run)
	if err := p.Set("true", "", config.EnvKindShell); err != nil {
		t.Fatal(err)
	}
	if got := p.Overlay(); len(got) != 1 || got[0] != "A=2" {
		t.Fatalf("overlay = %v", got)
	}
	s.actErr = errors.New("remote shell failed")
	if got := p.Overlay(); len(got) != 0 {
		t.Fatalf("overlay after a failed re-capture = %v", got)
	}
	if p.LastError() == nil {
		t.Fatal("the capture error was not recorded")
	}
}
