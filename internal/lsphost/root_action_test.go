package lsphost

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/lsp"
)

// TestServerActionRootUsesDocumentRoot: a server action from the active document picks
// that document's manager root, not an arbitrary manager from map iteration.
func TestServerActionRootUsesDocumentRoot(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	workspace := t.TempDir()
	rootA := filepath.Join(workspace, "a")
	rootB := filepath.Join(workspace, "b")
	port.SetRoot(workspace)
	port.setDoc(filepath.Join(rootA, "a.go"), lspDocRec{root: rootA, lang: "go"})
	port.setDoc(filepath.Join(rootB, "b.go"), lspDocRec{root: rootB, lang: "go"})
	if got := port.serverActionRoot(filepath.Join(rootA, "a.go"), "go"); got != rootA {
		t.Fatalf("root A action root = %q, want %q", got, rootA)
	}
	if got := port.serverActionRoot(filepath.Join(rootB, "b.go"), "go"); got != rootB {
		t.Fatalf("root B action root = %q, want %q", got, rootB)
	}
	if got := port.serverActionRoot("", "go"); got != workspace {
		t.Fatalf("no-document action root = %q, want the workspace root", got)
	}
}

// TestRestartServerForTargetsOneRoot: with two managers for the same language/name,
// restarting from a document in root A starts only root A's server.
func TestRestartServerForTargetsOneRoot(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	workspace := t.TempDir()
	rootA := filepath.Join(workspace, "a")
	rootB := filepath.Join(workspace, "b")
	port.SetRoot(workspace)
	port.mu.Lock()
	port.managers[rootA] = port.newManagerLocked(rootA)
	port.managers[rootB] = port.newManagerLocked(rootB)
	port.mu.Unlock()
	port.setDoc(filepath.Join(rootA, "a.go"), lspDocRec{root: rootA, lang: "go"})

	var roots []string
	port.SetSpawner(func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		roots = append(roots, cfg.RootURI)
		return nil, errors.New("captured spawn")
	})
	_ = port.RestartServerFor(filepath.Join(rootA, "a.go"), "go", "gopls")
	if len(roots) != 1 || roots[0] != lsp.PathURI(rootA) {
		t.Fatalf("restart spawn roots = %v, want only %q", roots, lsp.PathURI(rootA))
	}
}

// TestRemoteRootMarkersUsesAuthorityChecker: a remote path resolves its manager root
// through the authority checker, not the local filesystem.
func TestRemoteRootMarkersUsesAuthorityChecker(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	workspace := "/remote/ws"
	port.SetRoot(workspace)
	var checks []string
	port.SetRemoteMarkerChecker(func(_ context.Context, dir string, markers []string) (bool, error) {
		checks = append(checks, dir)
		return dir == workspace+"/sub", nil
	})
	var roots []string
	port.SetSpawner(func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		roots = append(roots, cfg.RootURI)
		return nil, errors.New("captured")
	})
	_, _ = port.Open(workspace+"/sub/a.go", "go", "package a\n")
	if len(roots) != 1 || roots[0] != lsp.PathURI(workspace+"/sub") {
		t.Fatalf("spawn roots = %v, want %q", roots, lsp.PathURI(workspace+"/sub"))
	}
	if len(checks) == 0 || checks[0] != workspace+"/sub" {
		t.Fatalf("authority checks = %v", checks)
	}
}

// TestRemoteRootMarkersCache: repeated opens under the same directory use the bounded
// marker cache; SetRoot clears it.
func TestRemoteRootMarkersCache(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	workspace := "/remote/ws"
	port.SetRoot(workspace)
	calls := 0
	port.SetRemoteMarkerChecker(func(_ context.Context, dir string, _ []string) (bool, error) {
		calls++
		return dir == workspace+"/sub", nil
	})
	port.SetSpawner(func(context.Context, lsp.Config) (*lsp.Client, error) { return nil, errors.New("captured") })
	_, _ = port.Open(workspace+"/sub/a.go", "go", "package a\n")
	_, _ = port.Open(workspace+"/sub/b.go", "go", "package b\n")
	if calls != 1 {
		t.Fatalf("authority marker checks = %d, want one cached answer", calls)
	}
	port.SetRoot("/remote/other")
	_, _ = port.Open(workspace+"/sub/c.go", "go", "package c\n")
	if calls != 2 {
		t.Fatalf("marker checks after SetRoot = %d, want the cache cleared", calls)
	}
}

// TestRemoteRootMarkersCacheRefreshKeepsOneOrderEntry: an expired answer is refreshed in
// place. Listing its key a second time would let a later eviction drop the fresh entry.
func TestRemoteRootMarkersCacheRefreshKeepsOneOrderEntry(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	checker := func(context.Context, string, []string) (bool, error) { return true, nil }
	markers := []string{"go.mod"}
	if _, err := port.markerAt(context.Background(), checker, "/remote/ws", markers, "go.mod"); err != nil {
		t.Fatal(err)
	}
	key := "/remote/ws\x00go.mod"
	port.rootCache[key] = remoteRootCacheEntry{ok: true, at: time.Now().Add(-2 * remoteRootCacheTTL)}
	if _, err := port.markerAt(context.Background(), checker, "/remote/ws", markers, "go.mod"); err != nil {
		t.Fatal(err)
	}
	if len(port.rootCacheOrder) != 1 {
		t.Fatalf("cache order = %v, want the key once", port.rootCacheOrder)
	}
	if time.Since(port.rootCache[key].at) > remoteRootCacheTTL {
		t.Fatal("the expired entry was not refreshed")
	}
}

// TestRemoteRootMarkersFallbackNotice: a failed remote marker read degrades to the
// workspace root and leaves an observable notice.
func TestRemoteRootMarkersFallbackNotice(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	workspace := "/remote/ws"
	port.SetRoot(workspace)
	port.SetRemoteMarkerChecker(func(context.Context, string, []string) (bool, error) {
		return false, errors.New("remote read denied")
	})
	var roots []string
	port.SetSpawner(func(_ context.Context, cfg lsp.Config) (*lsp.Client, error) {
		roots = append(roots, cfg.RootURI)
		return nil, errors.New("captured")
	})
	_, _ = port.Open(workspace+"/sub/a.go", "go", "package a\n")
	if len(roots) != 1 || roots[0] != lsp.PathURI(workspace) {
		t.Fatalf("fallback roots = %v, want the workspace root", roots)
	}
	if notice := port.TakeLSPNotice(); notice.Key != "gvim.notice.lsp.remote_root_failed" || notice.Vars["error"] == nil {
		t.Fatalf("fallback notice = %+v", notice)
	}
}

// TestRemoteRootMarkersDepthBound: an unbounded/deep remote path stops after the fixed
// depth budget and reports the fallback instead of walking forever.
func TestRemoteRootMarkersDepthBound(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	workspace := "/remote/ws"
	port.SetRoot(workspace)
	calls := 0
	port.SetRemoteMarkerChecker(func(context.Context, string, []string) (bool, error) {
		calls++
		return false, nil
	})
	port.SetSpawner(func(context.Context, lsp.Config) (*lsp.Client, error) { return nil, errors.New("captured") })
	deep := workspace
	for range 40 {
		deep += "/d"
	}
	_, _ = port.Open(deep+"/a.go", "go", "package a\n")
	if calls > 32 {
		t.Fatalf("authority marker checks = %d, want at most 32", calls)
	}
	if notice := port.TakeLSPNotice(); notice.Key != "gvim.notice.lsp.remote_root_depth" || notice.Vars["depth"] != remoteRootMaxDepth {
		t.Fatalf("depth notice = %+v", notice)
	}
}

// TestRemoteRootMarkersAbandonsOnWorkspaceChange: changing SetRoot while the probe is
// running makes the old scan abandon with a notice.
func TestRemoteRootMarkersAbandonsOnWorkspaceChange(t *testing.T) {
	port := NewFor(config.Defaults()).(*lspPort)
	workspace := "/remote/ws"
	port.SetRoot(workspace)
	port.SetRemoteMarkerChecker(func(context.Context, string, []string) (bool, error) {
		port.SetRoot("/remote/next")
		return false, nil
	})
	port.SetSpawner(func(context.Context, lsp.Config) (*lsp.Client, error) { return nil, errors.New("captured") })
	_, _ = port.Open(workspace+"/sub/a.go", "go", "package a\n")
	if notice := port.TakeLSPNotice(); notice.Key != "gvim.notice.lsp.remote_root_changed" {
		t.Fatalf("workspace-change notice = %+v", notice)
	}
}
