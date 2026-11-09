package app

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/env"
	"github.com/tbxark/gvim/internal/filewatch"
	"github.com/tbxark/gvim/plugin/hostos"
)

// These tests are race-detector targets: they run the watcher lifecycle, replanning and
// teardown concurrently with provider reads/events. They assert no panic/deadlock; the
// meaningful oracle is go test -race.
func TestLocalEnvWatchConcurrentReplanAndClose(t *testing.T) {
	root := t.TempDir()
	provider := env.New(root, []config.EnvDetector{{Name: "node", Markers: []string{".nvmrc"}, Kind: config.EnvKindShell, Snippet: "true"}}, func() bool { return true })
	fake := newFakeEnvFileWatch()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lw := startLocalEnvWatchWith(ctx, provider, root, fake, nil)
	if lw == nil {
		t.Fatal("no local watcher")
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	started := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		close(started)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			lw.Replan([]config.EnvDetector{{Name: "node", Markers: []string{".nvmrc", ".direnv"}, Kind: config.EnvKindShell}})
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		marker := filepath.Join(root, ".nvmrc")
		for {
			select {
			case <-stop:
				return
			case <-lw.ctx.Done():
				return
			case fake.events <- filewatch.Changed{Files: []string{marker}}:
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_, _ = provider.Detected()
			_ = provider.WatchNotice()
			_, _ = provider.Active()
		}
	}()
	<-started
	lw.Close()
	close(stop)
	wg.Wait()
}

func TestRemoteEnvWatchConcurrentReplanAndClose(t *testing.T) {
	var detect, capture atomic.Int32
	provider := envWatchProvider(t, &detect, &capture)
	raw := &fakeWatchFS{}
	slot := watchSlot(raw)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch := startRemoteEnvWatch(ctx, slot, provider, "/remote/proj")
	if watch == nil {
		t.Fatal("no remote watcher")
	}
	fw := raw.waitWatch(t, 1)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	started := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		close(started)
		for {
			select {
			case <-stop:
				return
			default:
			}
			watch.Replan([]config.EnvDetector{{Name: "direnv", Markers: []string{".envrc", ".direnv"}, Kind: config.EnvKindShell, Snippet: "true"}})
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			case <-watch.ctx.Done():
				return
			case fw.events <- hostos.WatchEvent{Path: "/remote/proj/.envrc", Kind: "write"}:
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_, _ = provider.Detected()
			_ = provider.WatchNotice()
			_, _ = provider.Active()
		}
	}()
	<-started
	watch.Close()
	close(stop)
	wg.Wait()
}
