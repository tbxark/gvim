package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/env"
)

func TestEnvPortEnvReloadTriggersWatcherRetry(t *testing.T) {
	root := t.TempDir()
	provider := env.New(root, nil, func() bool { return true })
	retried := make(chan struct{}, 2)
	port := envPort{
		provider: provider,
		level:    func() string { return "trusted" },
		retryWatch: func() {
			retried <- struct{}{}
		},
	}
	waitRetry := func() {
		t.Helper()
		select {
		case <-retried:
		case <-time.After(2 * time.Second):
			t.Fatal("env action did not trigger the watcher retry hook")
		}
	}
	if err := port.SetEnv("true", "", string(config.EnvKindShell)); err != nil {
		t.Fatal(err)
	}
	waitRetry()
	if err := port.ClearEnv(); err != nil {
		t.Fatal(err)
	}
	waitRetry()

	// An untrusted SetEnv must not trigger recovery (nothing changed).
	untrusted := env.New(root, nil, func() bool { return false })
	blocked := envPort{provider: untrusted, level: func() string { return "restricted" }, retryWatch: func() { retried <- struct{}{} }}
	if err := blocked.SetEnv("true", "", string(config.EnvKindShell)); err == nil {
		t.Fatal("untrusted SetEnv succeeded")
	}
	select {
	case <-retried:
		t.Fatal("a failed/untrusted env action triggered the retry hook")
	default:
	}
}

func TestEnvPortDetectedEnvTriggersNeededWatchRetry(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "not-yet")
	provider := env.New(root, []config.EnvDetector{{
		Name: "node", Markers: []string{".nvmrc"}, Kind: config.EnvKindShell, Snippet: "true",
	}}, func() bool { return true })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Simulate the initial root-Add failure (the real path is covered by the local watcher
	// retry test; this test drives the env-port read that Env: Reload performs first).
	provider.SetWatchRetryNeeded(true)
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".nvmrc"), []byte("20"), 0o600); err != nil {
		t.Fatal(err)
	}
	retry := &localEnvWatchRetry{
		ctx:   ctx,
		start: func() *localEnvWatch { return startLocalEnvWatch(ctx, provider, root) },
	}
	port := envPort{provider: provider, level: func() string { return "trusted" }, retryWatch: retry.Retry}
	// The env-manager's reload first reads detected(); that read must recover the watcher
	// even when it returns "no env detected" before any set/clear.
	if _, ok := port.DetectedEnv(); ok {
		t.Fatal("stale detection unexpectedly found a marker before the retry")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		det, ok := provider.Detected()
		if ok && det.Name == "node" && provider.WatchNotice() == "" && !provider.WatchRetryNeeded() {
			// A second detected() read is backed by a live watcher, so it must not start
			// another one.
			before := retry.Current()
			port.DetectedEnv()
			if retry.Current() != before {
				t.Fatal("detected() read replaced a live watcher")
			}
			before.Close()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if cur := retry.Current(); cur != nil {
		cur.Close()
	}
	t.Fatalf("detected() read did not recover the watcher: notice=%q retry=%v", provider.WatchNotice(), provider.WatchRetryNeeded())
}
