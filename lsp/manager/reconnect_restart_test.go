package manager

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// waitNoHandles waits until the manager has dropped the dead server handles, so a test
// calls RestartAfterReconnect in the state a carrier drop leaves behind.
func waitNoHandles(t *testing.T, m *Manager) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		m.pruneLocked()
		n := len(m.handles)
		m.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the server handle did not go away after the carrier drop")
}

// runningServers counts the manager's running servers.
func runningServers(m *Manager) int {
	n := 0
	for _, s := range m.Servers() {
		if s.State == ServerRunning {
			n++
		}
	}
	return n
}

// TestRestartAfterReconnectRestartsWithoutDuplicates checks the reconnect restart: a
// server that was running when the carrier dropped starts once more, the live one is not
// started a second time, and only one server is running afterwards.
func TestRestartAfterReconnectRestartsWithoutDuplicates(t *testing.T) {
	ctx := newTestContext(t)
	log := &startLog{t: t}
	m := newLoggedManager(log, map[string]ServerConfig{"go": {Command: []string{"gopls"}}})
	if err := m.Open(mgrAPath, "go", "x"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(log.pairs) != 1 || runningServers(m) != 1 {
		t.Fatalf("after Open: %d starts, %d running", len(log.pairs), runningServers(m))
	}
	_ = log.pairs[0].srvEnd.Close()
	waitNoHandles(t, m)
	n, err := m.RestartAfterReconnect(ctx)
	if err != nil || n != 1 {
		t.Fatalf("RestartAfterReconnect = %d, %v; want one restart", n, err)
	}
	if len(log.pairs) != 2 || runningServers(m) != 1 {
		t.Fatalf("after restart: %d starts, %d running; want one more start and one running", len(log.pairs), runningServers(m))
	}
	// A second call must not create a duplicate.
	n, err = m.RestartAfterReconnect(ctx)
	if err != nil || n != 0 {
		t.Fatalf("second RestartAfterReconnect = %d, %v; want a no-op", n, err)
	}
	if len(log.pairs) != 2 {
		t.Fatalf("a second reconnect started a duplicate (%d starts)", len(log.pairs))
	}
}

// TestRestartAfterReconnectHonorsTrust checks the spawn gate decides before a restart: a
// denied gate starts nothing and reports the reason.
func TestRestartAfterReconnectHonorsTrust(t *testing.T) {
	ctx := newTestContext(t)
	log := &startLog{t: t}
	m := newLoggedManager(log, map[string]ServerConfig{"go": {Command: []string{"gopls"}}})
	if err := m.Open(mgrAPath, "go", "x"); err != nil {
		t.Fatal(err)
	}
	_ = log.pairs[0].srvEnd.Close()
	waitNoHandles(t, m)
	m.SetGate(func(context.Context, []string, string, string) error { return errors.New("denied by trust") })
	n, err := m.RestartAfterReconnect(ctx)
	if n != 0 || err == nil || !strings.Contains(err.Error(), "denied by trust") {
		t.Fatalf("trust-denied restart = %d, %v; want no start and the denial", n, err)
	}
	if len(log.pairs) != 1 {
		t.Fatalf("a denied restart started %d servers", len(log.pairs)-1)
	}
}

// TestRestartAfterReconnectHonorsCancellation checks a cancelled context starts nothing.
func TestRestartAfterReconnectHonorsCancellation(t *testing.T) {
	ctx := newTestContext(t)
	log := &startLog{t: t}
	m := newLoggedManager(log, map[string]ServerConfig{"go": {Command: []string{"gopls"}}})
	if err := m.Open(mgrAPath, "go", "x"); err != nil {
		t.Fatal(err)
	}
	_ = log.pairs[0].srvEnd.Close()
	waitNoHandles(t, m)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	n, err := m.RestartAfterReconnect(canceled)
	if n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled restart = %d, %v; want a cancellation and no start", n, err)
	}
	if len(log.pairs) != 1 {
		t.Fatalf("a cancelled restart started %d servers", len(log.pairs)-1)
	}
}

// TestRestartAfterReconnectSkipsStoppedAndMuted checks the user's explicit choices are
// kept: a manually stopped server and a muted language are not restarted on reconnect.
func TestRestartAfterReconnectSkipsStoppedAndMuted(t *testing.T) {
	ctx := newTestContext(t)
	log := &startLog{t: t}
	m := newLoggedManager(log, map[string]ServerConfig{"go": {Command: []string{"gopls"}}})
	if err := m.Open(mgrAPath, "go", "x"); err != nil {
		t.Fatal(err)
	}
	if err := m.StopServer("go", ""); err != nil {
		t.Fatalf("StopServer: %v", err)
	}
	waitNoHandles(t, m)
	if n, err := m.RestartAfterReconnect(ctx); n != 0 || err != nil {
		t.Fatalf("a manually stopped server restarted: %d, %v", n, err)
	}
	// A muted language is not restarted either; un-muting and starting by hand is the
	// explicit action.
	m.SetLanguageEnabled("go", true)
	if err := m.StartLanguage("go"); err != nil {
		t.Fatalf("StartLanguage: %v", err)
	}
	m.SetLanguageEnabled("go", false)
	waitNoHandles(t, m)
	if n, err := m.RestartAfterReconnect(ctx); n != 0 || err != nil {
		t.Fatalf("a muted language restarted: %d, %v", n, err)
	}
}
