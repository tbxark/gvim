package app

// Real-fsnotify integration tests for the local env detector watcher. They use temporary
// directories and the production filewatch implementation (no fake watcher, no fixed
// sleeps): each assertion waits for a condition with a generous deadline, so a slow CI
// filesystem does not make them flaky. Covered: marker create/remove, a nested marker
// parent directory appearing after the watcher started (the root event must re-arm before
// the re-detection), and a real unwatchable root degrading to the explicit Env: Reload
// notice.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/env"
	"github.com/tbxark/gvim/internal/filewatch"
)

func waitLocalDetection(t *testing.T, provider *env.Provider, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		det, ok := provider.Detected()
		if want == "" {
			if !ok {
				return
			}
		} else if ok && det.Name == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	det, ok := provider.Detected()
	t.Fatalf("local detection = %+v, %v; want %q", det, ok, want)
}

func testDetector(name, marker string) []config.EnvDetector {
	return []config.EnvDetector{{Name: name, Markers: []string{marker}, Kind: config.EnvKindShell, Snippet: "true"}}
}

func startRealLocalEnvWatch(t *testing.T, provider *env.Provider, root string) *localEnvWatch {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	lw := startLocalEnvWatch(ctx, provider, root)
	if lw == nil {
		if notice := provider.WatchNotice(); notice != "" {
			t.Skipf("fsnotify is unavailable in this environment: %s", notice)
		}
		t.Skip("fsnotify is unavailable in this environment")
	}
	t.Cleanup(lw.Close)
	return lw
}

func TestLocalEnvWatchRealFsnotifyMarkerLifecycle(t *testing.T) {
	root := t.TempDir()
	provider := env.New(root, testDetector("node", ".nvmrc"), func() bool { return true })
	_ = startRealLocalEnvWatch(t, provider, root)
	marker := filepath.Join(root, ".nvmrc")
	if err := os.WriteFile(marker, []byte("20"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitLocalDetection(t, provider, "node")
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	waitLocalDetection(t, provider, "")
	if notice := provider.WatchNotice(); notice != "" {
		t.Fatalf("healthy real-fsnotify watcher notice = %q", notice)
	}
}

func TestLocalEnvWatchRealFsnotifyNestedParentAppears(t *testing.T) {
	root := t.TempDir()
	provider := env.New(root, testDetector("custom", "config/env.toml"), func() bool { return true })
	_ = startRealLocalEnvWatch(t, provider, root)
	// The config parent does not exist at watcher start: the root watch must re-arm the
	// nested directory before the re-detection, otherwise the newly created marker could
	// fall into the refresh/Add gap.
	dir := filepath.Join(root, "config")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "env.toml")
	if err := os.WriteFile(marker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitLocalDetection(t, provider, "custom")
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	waitLocalDetection(t, provider, "")
}

func TestLocalEnvWatchRealFsnotifyUnwatchableRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	provider := env.New(missing, testDetector("node", ".nvmrc"), func() bool { return true })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	lw := startLocalEnvWatch(ctx, provider, missing)
	if lw != nil {
		lw.Close()
		t.Skip("fsnotify accepted a nonexistent root on this platform")
	}
	notice := provider.WatchNotice()
	if !strings.Contains(notice, "Local environment:") || !strings.Contains(notice, "Env: Reload") {
		t.Fatalf("unwatchable-root notice = %q", notice)
	}
	if strings.Contains(notice, "Remote environment:") {
		t.Fatalf("unwatchable-root notice leaked remote wording: %q", notice)
	}
}

var _ = filewatch.Changed{}
