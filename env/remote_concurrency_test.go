package env

import (
	"context"
	"sync"
	"testing"

	"github.com/tbxark/gvim/config"
)

func TestRemoteConcurrentDetectionLifecycle(t *testing.T) {
	detectors := []config.EnvDetector{{Name: "node", Markers: []string{".nvmrc"}, Kind: config.EnvKindShell, Snippet: "true"}}
	run := func(context.Context, string) (string, error) { return "", nil }
	p := NewRemote("/remote/proj", detectors, func() bool { return true }, run)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				p.SetDetectors([]config.EnvDetector{{Name: "node", Markers: []string{".nvmrc"}, Kind: config.EnvKindShell, Snippet: "true"}})
				p.Warm(context.Background())
				_ = p.Overlay()
				p.Refresh()
				_, _ = p.Detected()
				_ = p.Detectors()
			}
		}()
	}
	wg.Wait()
}

func TestLocalConcurrentDetectionLifecycle(t *testing.T) {
	root := t.TempDir()
	detectors := []config.EnvDetector{{Name: "node", Markers: []string{".nvmrc"}, Kind: config.EnvKindShell, Snippet: "true"}}
	p := New(root, detectors, func() bool { return true })
	p.SetRunner(func(string, string) (string, error) { return "", nil })
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				p.SetDetectors([]config.EnvDetector{{Name: "node", Markers: []string{".nvmrc"}, Kind: config.EnvKindShell, Snippet: "true"}})
				p.RefreshDetection()
				_ = p.Overlay()
				_, _ = p.Detected()
				_, _ = p.Active()
				_ = p.WatchNotice()
			}
		}()
	}
	wg.Wait()
}
