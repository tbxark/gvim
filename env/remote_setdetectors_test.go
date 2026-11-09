package env

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/tbxark/gvim/config"
)

func TestRemoteSetDetectorsInvalidatesDetection(t *testing.T) {
	s := &remoteScript{markers: "0\n"}
	p := NewRemote("/remote/proj", direnvDetectors(), func() bool { return true }, s.run)
	p.Warm(context.Background())
	if det, ok := p.Detected(); !ok || det.Name != "direnv" {
		t.Fatalf("initial detected = %+v, %v", det, ok)
	}
	p.SetDetectors([]config.EnvDetector{{
		Name: "node", Markers: []string{".nvmrc"}, Kind: config.EnvKindShell, Snippet: "true",
	}})
	if det, ok := p.Detected(); ok || det.Name != "" {
		t.Fatalf("SetDetectors did not invalidate detection: %+v, %v", det, ok)
	}
	p.Warm(context.Background())
	det, ok := p.Detected()
	if !ok || det.Name != "node" {
		t.Fatalf("detected after SetDetectors = %+v, %v", det, ok)
	}
	if calls := s.recorded(); len(calls) != 2 || !strings.Contains(calls[1], ".nvmrc") {
		t.Fatalf("probes = %v, want the second to use the new marker", calls)
	}
	// A nil/empty list restores the built-in detectors.
	p.SetDetectors(nil)
	if got := p.Detectors(); len(got) != len(config.DefaultEnvDetectors()) {
		t.Fatalf("nil SetDetectors = %d detectors, want defaults", len(got))
	}
}

func TestRemoteSetDetectorsIgnoresInFlightOldProbe(t *testing.T) {
	var mu sync.Mutex
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	first := true
	run := func(context.Context, string) (string, error) {
		mu.Lock()
		f := first
		first = false
		mu.Unlock()
		if f {
			started <- struct{}{}
			<-release
			return "0\n", nil // the old detector matched
		}
		return "0\n", nil
	}
	p := NewRemote("/remote/proj", direnvDetectors(), func() bool { return true }, run)
	done := make(chan struct{})
	go func() {
		p.Warm(context.Background())
		close(done)
	}()
	<-started
	// The config reload lands while the old probe is running.
	p.SetDetectors([]config.EnvDetector{{
		Name: "node", Markers: []string{".nvmrc"}, Kind: config.EnvKindShell, Snippet: "true",
	}})
	close(release)
	<-done
	if det, ok := p.Detected(); ok || det.Name != "" {
		t.Fatalf("the in-flight old probe was cached after SetDetectors: %+v, %v", det, ok)
	}
	// The next Warm probes the new detector set.
	p.Warm(context.Background())
	if det, ok := p.Detected(); !ok || det.Name != "node" {
		t.Fatalf("post-change detection = %+v, %v", det, ok)
	}
}
