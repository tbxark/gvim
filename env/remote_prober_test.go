package env

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/tbxark/gvim/config"
)

type fakeMarkerProber struct {
	mu     sync.Mutex
	paths  [][]string
	exists map[string]bool
	err    error
}

func (f *fakeMarkerProber) probe(_ context.Context, root string, paths []string) (map[string]bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, append([]string(nil), paths...))
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]bool{}
	for p := range f.exists {
		out[p] = true
	}
	return out, nil
}

func TestRemoteDetectionUsesAuthorityProbeBeforeTrust(t *testing.T) {
	prober := &fakeMarkerProber{exists: map[string]bool{".envrc": true}}
	runCalls := 0
	run := func(context.Context, string) (string, error) {
		runCalls++
		return "", errors.New("the shell probe must not run untrusted")
	}
	p := NewRemote("/remote/proj", direnvDetectors(), func() bool { return false }, run)
	p.SetMarkerProber(prober.probe)
	p.Warm(context.Background())
	det, ok := p.Detected()
	if !ok || det.Name != "direnv" {
		t.Fatalf("untrusted shell-free detection = %+v, %v", det, ok)
	}
	if runCalls != 0 {
		t.Fatalf("an untrusted workspace ran the shell probe %d times", runCalls)
	}
	if notice := p.DetectionNotice(); notice != "" {
		t.Fatalf("healthy shell-free detection notice = %q", notice)
	}
	prober.mu.Lock()
	paths := append([]string(nil), prober.paths[0]...)
	prober.mu.Unlock()
	if strings.Join(paths, ",") != ".envrc" {
		t.Fatalf("prober paths = %v", paths)
	}

	// Detection result and paths include require evidence, exactly like the shell probe.
	prober = &fakeMarkerProber{exists: map[string]bool{".venv": true, ".venv/bin/python": true}}
	p = NewRemote("/remote/proj", venvDetectors(), func() bool { return false }, run)
	p.SetMarkerProber(prober.probe)
	p.Warm(context.Background())
	if det, ok := p.Detected(); !ok || det.Name != ".venv" {
		t.Fatalf("venv shell-free detection = %+v, %v", det, ok)
	}
	prober.mu.Lock()
	paths = append([]string(nil), prober.paths[0]...)
	prober.mu.Unlock()
	if !strings.Contains(strings.Join(paths, ","), ".venv/bin/python") {
		t.Fatalf("prober paths lack the require evidence: %v", paths)
	}
}

func TestRemoteAuthorityProbeFailureIsExplicit(t *testing.T) {
	prober := &fakeMarkerProber{err: errors.New("authority file surface is disconnected")}
	p := NewRemote("/remote/proj", direnvDetectors(), func() bool { return false }, nil)
	p.SetMarkerProber(prober.probe)
	p.Warm(context.Background())
	if det, ok := p.Detected(); ok || det.Name != "" {
		t.Fatalf("failed shell-free detection = %+v, %v", det, ok)
	}
	if notice := p.DetectionNotice(); !strings.Contains(notice, "marker detection failed") || !strings.Contains(notice, "disconnected") {
		t.Fatalf("failure notice = %q", notice)
	}
	prober.err = nil
	prober.exists = map[string]bool{".envrc": true}
	p.Warm(context.Background())
	if det, ok := p.Detected(); !ok || det.Name != "direnv" {
		t.Fatalf("recovered shell-free detection = %+v, %v", det, ok)
	}
}

func TestRemoteAuthorityProbeIsPathBounded(t *testing.T) {
	var dets []config.EnvDetector
	for i := 0; i < MaxMarkerProbePaths+1; i++ {
		dets = append(dets, config.EnvDetector{
			Name: fmt.Sprintf("d%d", i), Markers: []string{fmt.Sprintf("marker-%d", i)},
			Kind: config.EnvKindShell,
		})
	}
	prober := &fakeMarkerProber{}
	p := NewRemote("/remote/proj", dets, func() bool { return false }, nil)
	p.SetMarkerProber(prober.probe)
	p.Warm(context.Background())
	prober.mu.Lock()
	calls := len(prober.paths)
	prober.mu.Unlock()
	if calls != 0 {
		t.Fatalf("the prober ran %d times past the path bound", calls)
	}
	if notice := p.DetectionNotice(); !strings.Contains(notice, "exceed") {
		t.Fatalf("path-bound notice = %q", notice)
	}
}

func TestRemoteDetectionTrustedFallbackToAuthorityProbe(t *testing.T) {
	// A trusted workspace with no shell runner (tests/embedding) can still use the
	// shell-free prober rather than failing detection.
	prober := &fakeMarkerProber{exists: map[string]bool{".envrc": true}}
	p := NewRemote("/remote/proj", direnvDetectors(), func() bool { return true }, nil)
	p.SetMarkerProber(prober.probe)
	p.Warm(context.Background())
	if det, ok := p.Detected(); !ok || det.Name != "direnv" {
		t.Fatalf("trusted fallback detection = %+v, %v", det, ok)
	}
}
