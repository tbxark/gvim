package app

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/env"
	"github.com/tbxark/gvim/plugin/hostos"
)

func envWatchProvider(t *testing.T, detect, capture *atomic.Int32) *env.Remote {
	t.Helper()
	run := func(context.Context, string) (string, error) {
		detect.Add(1)
		return "0\n", nil
	}
	captureRun := func(_ context.Context, command string) (string, error) {
		capture.Add(1)
		if strings.Contains(command, "set -a;") {
			return "HOME=/home/u\x00PATH=/venv/bin\x00REMOTE_ONLY=1\x00", nil
		}
		return "HOME=/home/u\x00PATH=/usr/bin\x00", nil
	}
	detectors := []config.EnvDetector{{Name: "direnv", Markers: []string{".envrc"}, Kind: config.EnvKindShell, Snippet: "eval \"$(direnv export sh)\""}}
	provider := env.NewRemote("/remote/proj", detectors, func() bool { return true }, run, captureRun)
	if err := provider.Set("eval \"$(direnv export sh)\"", "", config.EnvKindShell); err != nil {
		t.Fatal(err)
	}
	provider.Warm(context.Background())
	return provider
}

func waitEnvWatchRefresh(t *testing.T, detect, capture *atomic.Int32, initialDetect, initialCapture int32) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if detect.Load() > initialDetect && capture.Load() >= initialCapture+2 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("env refresh did not run: detect %d→%d capture %d→%d", initialDetect, detect.Load(), initialCapture, capture.Load())
}

func TestRemoteEnvWatchInvalidatesOnEnvInputChange(t *testing.T) {
	var detect, capture atomic.Int32
	provider := envWatchProvider(t, &detect, &capture)
	initialDetect, initialCapture := detect.Load(), capture.Load()
	fs := &fakeWatchFS{}
	slot := watchSlot(fs)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch := startRemoteEnvWatch(ctx, slot, provider, "/remote/proj")
	if watch == nil {
		t.Fatal("no remote env watch")
	}
	defer watch.Close()
	fw := fs.waitWatch(t, 1)
	fs.mu.Lock()
	dir := fs.dirs[0]
	fs.mu.Unlock()
	if dir != "/remote/proj" {
		t.Fatalf("watched %q, want the remote workspace root", dir)
	}

	fw.push(hostos.WatchEvent{Path: "/remote/proj/.envrc", Kind: "write"})
	waitEnvWatchRefresh(t, &detect, &capture, initialDetect, initialCapture)

	// An unrelated file does not invalidate the environment.
	beforeDetect, beforeCapture := detect.Load(), capture.Load()
	fw.push(hostos.WatchEvent{Path: "/remote/proj/main.go", Kind: "write"})
	time.Sleep(150 * time.Millisecond)
	if detect.Load() != beforeDetect || capture.Load() != beforeCapture {
		t.Fatalf("an unrelated change refreshed the env: detect %d→%d capture %d→%d", beforeDetect, detect.Load(), beforeCapture, capture.Load())
	}

	// A change inside .venv is a directory-mtime event on the root watcher.
	fw.push(hostos.WatchEvent{Path: "/remote/proj/.venv", Kind: "write"})
	waitEnvWatchRefresh(t, &detect, &capture, beforeDetect, beforeCapture)
	if notice := provider.WatchNotice(); notice != "" {
		t.Fatalf("watch notice while watching = %q", notice)
	}
}

func TestRemoteEnvWatchDegradesWithoutCapability(t *testing.T) {
	var detect, capture atomic.Int32
	provider := envWatchProvider(t, &detect, &capture)
	fs := &noWatchFS{fakeWatchFS: &fakeWatchFS{}}
	slot := watchSlot(fs.fakeWatchFS)
	slot.FileSystem = fs
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch := startRemoteEnvWatch(ctx, slot, provider, "/remote/proj")
	if watch == nil {
		t.Fatal("no remote env watch")
	}
	defer watch.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if notice := provider.WatchNotice(); strings.Contains(notice, "cannot watch files") {
			if !strings.Contains(notice, "Env: Reload") {
				t.Fatalf("degradation notice = %q", notice)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("unsupported watch did not record a degradation notice: %q", provider.WatchNotice())
}

func TestRemoteEnvWatchCancellationStopsWatcher(t *testing.T) {
	var detect, capture atomic.Int32
	provider := envWatchProvider(t, &detect, &capture)
	fs := &fakeWatchFS{}
	slot := watchSlot(fs)
	ctx, cancel := context.WithCancel(context.Background())
	watch := startRemoteEnvWatch(ctx, slot, provider, "/remote/proj")
	fw := fs.waitWatch(t, 1)
	cancel()
	watch.Close()
	fw.stopped(t)
}

// noWatchFS is the same file surface without the Watch capability.
type noWatchFS struct {
	*fakeWatchFS
}

func (noWatchFS) Watch(context.Context, string, hostos.WatchOptions) (hostos.Watch, error) {
	return nil, authority.ErrUnsupportedFileIO
}

var _ = errors.New

// envWatchFS additionally reports the .venv directory once the test creates it, so the
// watcher can arm the nested watch after the root event.
type envWatchFS struct {
	*fakeWatchFS
	nested bool
}

func (f *envWatchFS) Stat(_ context.Context, p string) (hostos.FileStat, error) {
	if f.nested && p == "/remote/proj/.venv" {
		return hostos.FileStat{Kind: "dir"}, nil
	}
	return hostos.FileStat{}, errors.New("not needed by the env-watch test")
}

func TestRemoteEnvWatchAddsNestedInputDirectory(t *testing.T) {
	var detect, capture atomic.Int32
	provider := envWatchProvider(t, &detect, &capture)
	raw := &fakeWatchFS{}
	fs := &envWatchFS{fakeWatchFS: raw}
	slot := watchSlot(raw)
	slot.FileSystem = fs
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watch := startRemoteEnvWatch(ctx, slot, provider, "/remote/proj")
	defer watch.Close()
	root := raw.waitWatch(t, 1)

	// The .venv directory appears: the root event invalidates the environment and the
	// watcher re-arms including the new directory.
	fs.nested = true
	initialDetect, initialCapture := detect.Load(), capture.Load()
	root.push(hostos.WatchEvent{Path: "/remote/proj/.venv", Kind: "create"})
	waitEnvWatchRefresh(t, &detect, &capture, initialDetect, initialCapture)
	nested := raw.waitWatch(t, 2)
	raw.mu.Lock()
	dirs := append([]string(nil), raw.dirs...)
	raw.mu.Unlock()
	if len(dirs) != 3 || dirs[0] != "/remote/proj" || dirs[1] != "/remote/proj" || dirs[2] != "/remote/proj/.venv" {
		t.Fatalf("watched dirs = %v, want a re-armed root then .venv", dirs)
	}

	// A content-only change to .venv/pyvenv.cfg now reaches the nested watch and
	// invalidates the environment without needing a root entry change.
	beforeDetect, beforeCapture := detect.Load(), capture.Load()
	nested.push(hostos.WatchEvent{Path: "/remote/proj/.venv/pyvenv.cfg", Kind: "write"})
	waitEnvWatchRefresh(t, &detect, &capture, beforeDetect, beforeCapture)
}
