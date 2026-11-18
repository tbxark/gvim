package app

import (
	"path/filepath"
	goruntime "runtime"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/trust"
)

// TestTrustDecideStoreWriteDoesNotBlockState checks the store write runs outside the
// lock State takes on the update goroutine using deterministic channel handshakes on
// the writeStore hook instead of FIFOs or sleeps.
func TestTrustDecideStoreWriteDoesNotBlockState(t *testing.T) {
	c := newTrustController(
		trust.NewStore(filepath.Join(t.TempDir(), "trust.json")),
		trust.State{Root: t.TempDir(), Level: trust.Restricted, Decided: true},
	)
	entered := make(chan struct{})
	release := make(chan struct{})
	c.writeStore = func(string, string, trust.Level) error {
		close(entered)
		<-release
		return nil
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.Decide(trust.Trusted)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Decide did not reach writeStore")
	}
	got := make(chan trust.State, 1)
	go func() { got <- c.State() }()
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("State blocked while Decide wrote the store")
	}
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Decide did not finish")
	}
}

// TestTrustConcurrentDecideSupersededByRestrict checks that a slow Trusted store
// write cannot overwrite a newer Restricted decision or corrupt the persisted level.
func TestTrustConcurrentDecideSupersededByRestrict(t *testing.T) {
	store := trust.NewStore(filepath.Join(t.TempDir(), "trust.json"))
	root := t.TempDir()
	c := newTrustController(store, trust.State{Root: root, Level: trust.Restricted, Decided: true})

	entered := make(chan struct{})
	release := make(chan struct{})
	var writes []trust.Level
	var writesMu sync.Mutex
	c.writeStore = func(_, r string, lvl trust.Level) error {
		if lvl == trust.Trusted {
			close(entered)
			<-release
		}
		writesMu.Lock()
		writes = append(writes, lvl)
		writesMu.Unlock()
		return store.Set(r, lvl)
	}

	grantErr := make(chan error, 1)
	go func() {
		_, err := c.Decide(trust.Trusted)
		grantErr <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("slow Decide(Trusted) did not enter writeStore")
	}

	restrictDone := make(chan error, 1)
	go func() {
		_, err := c.Decide(trust.Restricted)
		restrictDone <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.mu.Lock()
		tr := c.transition
		c.mu.Unlock()
		if tr >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Decide(Restricted) did not advance transition")
		}
		goruntime.Gosched()
	}

	close(release)
	if err := <-grantErr; err == nil {
		t.Fatal("superseded Decide(Trusted) succeeded, want superseded error")
	}
	if err := <-restrictDone; err != nil {
		t.Fatalf("Decide(Restricted): %v", err)
	}
	if got := c.State().Level; got != trust.Restricted {
		t.Fatalf("live level = %s, want %s", got, trust.Restricted)
	}
	if level, decided := store.LevelFor(root); !decided || level != trust.Restricted {
		t.Fatalf("persisted store level = (%s, %v), want (%s, true)", level, decided, trust.Restricted)
	}
}
