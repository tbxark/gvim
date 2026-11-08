package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/theme"
)

// batchMessages runs a command and flattens its batch, so a test can inspect the messages
// the root scheduled without a running program.
func batchMessages(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, batchMessages(t, c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// TestReconnectRunsAfterReconnectWorkOnSuccessOnly checks the post-reconnect LSP work: it
// is scheduled off the update goroutine only after a successful reconnect, its message
// reports the restarted count, and a failed reconnect runs nothing.
func TestReconnectRunsAfterReconnectWorkOnSuccessOnly(t *testing.T) {
	calls := 0
	m := New(Deps{Editor: newFakeEditor("x"), Theme: theme.Theme{}, Clock: testClock, Keys: fakeKeymap{}})
	m.SetAuthorityHooks(AuthorityHooks{
		AfterReconnect: func() (int, error) {
			calls++
			return 2, nil
		},
	})
	// A failed reconnect runs no post-reconnect work.
	if _, cmd := m.Update(authorityReconnectedMsg{err: errors.New("dial failed")}); cmd != nil {
		t.Fatal("a failed reconnect scheduled post-reconnect work")
	}
	if calls != 0 {
		t.Fatal("a failed reconnect ran the hook")
	}
	// A successful reconnect schedules it as a command (not on the update goroutine).
	_, cmd := m.Update(authorityReconnectedMsg{})
	if cmd == nil {
		t.Fatal("a successful reconnect scheduled no post-reconnect work")
	}
	if calls != 0 {
		t.Fatal("the hook ran on the update goroutine")
	}
	var done *authorityRestartDoneMsg
	for _, msg := range batchMessages(t, cmd) {
		if r, ok := msg.(authorityRestartDoneMsg); ok {
			copy := r
			done = &copy
		}
	}
	if done == nil {
		t.Fatal("no restart result was scheduled")
	}
	if calls != 1 || done.count != 2 || done.err != nil {
		t.Fatalf("hook calls = %d, result = %+v; want one call reporting 2 servers", calls, done)
	}
	m.Update(*done)
	// One notice reports the refreshed workspace and the restarted servers.
	if !strings.Contains(m.notice, "refreshed") || !strings.Contains(m.notice, "2") {
		t.Fatalf("notice after the restart = %q, want one refreshed-workspace notice with the server count", m.notice)
	}
	// A reconnect without post-reconnect work still reports itself once.
	m.SetAuthorityHooks(AuthorityHooks{})
	_, _ = m.Update(authorityReconnectedMsg{})
	if !strings.Contains(m.notice, "SSH reconnected") {
		t.Fatalf("notice without post-reconnect work = %q", m.notice)
	}
}

// TestReconnectRestartFailureIsANotice checks a failed post-reconnect restart is reported,
// not swallowed.
func TestReconnectRestartFailureIsANotice(t *testing.T) {
	m := New(Deps{Editor: newFakeEditor("x"), Theme: theme.Theme{}, Clock: testClock, Keys: fakeKeymap{}})
	m.SetAuthorityHooks(AuthorityHooks{
		AfterReconnect: func() (int, error) { return 0, errors.New("gate denied") },
	})
	_, cmd := m.Update(authorityReconnectedMsg{})
	msgs := batchMessages(t, cmd)
	var done *authorityRestartDoneMsg
	for _, msg := range msgs {
		if r, ok := msg.(authorityRestartDoneMsg); ok {
			copy := r
			done = &copy
		}
	}
	if done == nil {
		t.Fatal("no restart result was scheduled")
	}
	m.Update(*done)
	if !strings.Contains(m.notice, "refresh failed") || !strings.Contains(m.notice, "gate denied") {
		t.Fatalf("notice after a failed restart = %q, want one refreshed-workspace notice naming the failure", m.notice)
	}
}
