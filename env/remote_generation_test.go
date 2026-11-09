package env

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/tbxark/gvim/config"
)

func remoteGenDetectors() []config.EnvDetector {
	return []config.EnvDetector{{Name: "node", Markers: []string{".nvmrc"}, Kind: config.EnvKindShell, Snippet: "true"}}
}

func TestRemoteRefreshDiscardsInFlightProbe(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	run := func(context.Context, string) (string, error) {
		first := false
		once.Do(func() { first = true })
		if first {
			close(started)
			<-release
		}
		return "0\n", nil
	}
	p := NewRemote("/remote/proj", remoteGenDetectors(), func() bool { return true }, run)
	done := make(chan struct{})
	go func() {
		p.Warm(context.Background())
		close(done)
	}()
	<-started
	// Invalidate while the old probe is in flight.
	p.Refresh()
	close(release)
	<-done
	if det, ok := p.Detected(); ok || det.Name != "" {
		t.Fatalf("the probe started before Refresh was cached: %+v, %v", det, ok)
	}
	p.Warm(context.Background())
	if det, ok := p.Detected(); !ok || det.Name != "node" {
		t.Fatalf("post-refresh detection = %+v, %v", det, ok)
	}
}

func TestRemoteSetRootDiscardsInFlightProbe(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	started := make(chan struct{})
	release := make(chan struct{})
	first := true
	run := func(_ context.Context, command string) (string, error) {
		mu.Lock()
		calls = append(calls, command)
		isFirst := first
		first = false
		mu.Unlock()
		if isFirst {
			close(started)
			<-release
		}
		return "0\n", nil
	}
	p := NewRemote("/remote/old", remoteGenDetectors(), func() bool { return true }, run)
	done := make(chan struct{})
	go func() {
		p.Warm(context.Background())
		close(done)
	}()
	<-started
	// The connection resolved a different root while the old probe was in flight.
	p.SetRoot("/remote/new")
	close(release)
	<-done
	if det, ok := p.Detected(); ok || det.Name != "" {
		t.Fatalf("the old-root probe was cached after SetRoot: %+v, %v", det, ok)
	}
	p.Warm(context.Background())
	if det, ok := p.Detected(); !ok || det.Name != "node" {
		t.Fatalf("new-root detection = %+v, %v", det, ok)
	}
	mu.Lock()
	last := calls[len(calls)-1]
	mu.Unlock()
	if !strings.Contains(last, "/remote/new") {
		t.Fatalf("post-SetRoot probe command = %q, want the new root", last)
	}
}
