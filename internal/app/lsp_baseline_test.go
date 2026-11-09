package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/trust"
)

// fakeLSPConfigSetter records the configs the baseline worker applies. The optional block
// channel stalls the first apply so a test can order reload/trust updates around it.
type fakeLSPConfigSetter struct {
	mu      sync.Mutex
	got     []config.Config
	started chan struct{}
	release chan struct{}
	calls   int
}

func (f *fakeLSPConfigSetter) SetConfig(cfg config.Config) {
	f.mu.Lock()
	f.calls++
	call := f.calls
	if call == 1 {
		if f.started != nil {
			close(f.started)
		}
		if f.release != nil {
			f.mu.Unlock()
			<-f.release
			f.mu.Lock()
		}
	}
	f.got = append(f.got, cfg)
	f.mu.Unlock()
}

func (f *fakeLSPConfigSetter) last() (config.Config, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.got) == 0 {
		return config.Config{}, false
	}
	return f.got[len(f.got)-1], true
}

func waitBaselineApplied(t *testing.T, b *lspBaselineUpdater) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if b.Applied() >= b.Seq() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("baseline did not apply: applied %d seq %d", b.Applied(), b.Seq())
}

// TestLSPBaselineLatestConfigWinsOverStaleTrustCallback is the controlled ordering
// regression for the audit: a trust update arrives while C1 is being applied and C2 has
// already been installed as the newest config; the stale C1 must never be the final
// baseline, and the latest config+trust snapshot is applied last.
func TestLSPBaselineLatestConfigWinsOverStaleTrustCallback(t *testing.T) {
	fake := &fakeLSPConfigSetter{started: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := newLSPBaselineUpdater(ctx, fake, config.Defaults(), trust.State{Level: trust.Trusted})
	if b == nil {
		t.Fatal("baseline updater was not built")
	}
	c1 := config.Defaults()
	c1.LSP = map[string][]config.LSPServer{"go": {{Command: "old-go"}}}
	c2 := config.Defaults()
	c2.LSP = map[string][]config.LSPServer{"go": {{Command: "new-go"}}}

	b.UpdateConfig(c1, trust.State{Level: trust.Trusted})
	select {
	case <-fake.started:
	case <-time.After(3 * time.Second):
		t.Fatal("the first apply did not start")
	}
	// C1 is stalled in SetConfig. The newer config arrives, then a trust callback (which
	// used to read the old settings snapshot). The worker must end on C2, not C1.
	b.UpdateConfig(c2, trust.State{Level: trust.Restricted})
	b.UpdateTrust(trust.State{Level: trust.Restricted})
	close(fake.release)
	waitBaselineApplied(t, b)
	got, ok := fake.last()
	if !ok {
		t.Fatal("no config applied")
	}
	if got.LSPEnabled {
		t.Fatal("the final baseline ignored the live Restricted gate")
	}
	if list := got.LSP["go"]; len(list) != 1 || list[0].Command != "new-go" {
		t.Fatalf("final baseline LSP[go] = %+v, want the newer C2 config", list)
	}
}

// TestLSPBaselineUpdateDoesNotBlockCaller checks UI non-blocking: with SetConfig stalled,
// UpdateConfig/UpdateTrust return promptly because only the worker applies.
func TestLSPBaselineUpdateDoesNotBlockCaller(t *testing.T) {
	fake := &fakeLSPConfigSetter{started: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := newLSPBaselineUpdater(ctx, fake, config.Defaults(), trust.State{Level: trust.Trusted})
	b.UpdateConfig(config.Defaults(), trust.State{Level: trust.Trusted})
	select {
	case <-fake.started:
	case <-time.After(3 * time.Second):
		t.Fatal("the worker did not enter SetConfig")
	}
	done := make(chan struct{})
	go func() {
		b.UpdateConfig(config.Defaults(), trust.State{Level: trust.Restricted})
		b.UpdateTrust(trust.State{Level: trust.Restricted})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("baseline Update blocked on the stalled SetConfig (UI goroutine would block)")
	}
	close(fake.release)
	waitBaselineApplied(t, b)
}
