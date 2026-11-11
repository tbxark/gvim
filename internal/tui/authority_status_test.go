package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/prompt"
)

// TestAuthorityStatusSegment checks the authority segment of a remote launch, connected
// and disconnected.
func TestAuthorityStatusSegment(t *testing.T) {
	m := statusViewModel(t, nil)
	m.SetAuthorityHooks(AuthorityHooks{Status: func() AuthorityStatus {
		return AuthorityStatus{Text: "SSH:user@host", Connected: true}
	}})
	if row := statusRowText(m); !strings.Contains(row, "SSH:user@host") {
		t.Fatalf("status row %q has no authority segment", row)
	}
	m.SetAuthorityHooks(AuthorityHooks{Status: func() AuthorityStatus {
		return AuthorityStatus{Text: "SSH:user@host", Connected: false, Reason: "connection lost"}
	}})
	if row := statusRowText(m); !strings.Contains(row, "SSH:user@host · disconnected") {
		t.Fatalf("disconnected status row %q has no indicator", row)
	}
	// A local launch (no hooks) shows nothing.
	local := statusViewModel(t, nil)
	if row := statusRowText(local); strings.Contains(row, "SSH:") {
		t.Fatalf("a local launch shows an authority segment: %q", row)
	}
}

// TestAuthorityReconnectOnActivate checks the activation hook reconnects only while
// disconnected and reports its outcome.
func TestAuthorityReconnectOnActivate(t *testing.T) {
	m := statusViewModel(t, nil)
	connected := true
	reconnects := 0
	m.SetAuthorityHooks(AuthorityHooks{
		Status: func() AuthorityStatus { return AuthorityStatus{Text: "SSH:u@h", Connected: connected} },
		Reconnect: func(context.Context, string) error {
			reconnects++
			connected = true
			return nil
		},
	})
	// Connected: activation does nothing.
	if cmd := m.beginAuthorityReconnect(); cmd != nil {
		t.Fatal("activation reconnected a connected authority")
	}
	connected = false
	cmd := m.beginAuthorityReconnect()
	if cmd == nil {
		t.Fatal("activation did not reconnect")
	}
	msg, ok := cmd().(authorityReconnectedMsg)
	if !ok || msg.err != nil {
		t.Fatalf("reconnect result = %#v", msg)
	}
	m.Update(msg)
	if reconnects != 1 {
		t.Fatalf("reconnects = %d, want 1", reconnects)
	}
	if m.notice == "" {
		t.Fatal("the reconnect outcome is not on the status row")
	}
}

// TestAuthorityPasswordPrompt checks a credential failure asks for a password through the
// existing prompt, and a submitted password reconnects with it.
func TestAuthorityPasswordPrompt(t *testing.T) {
	m := statusViewModel(t, nil)
	var got string
	m.SetAuthorityHooks(AuthorityHooks{
		Status:        func() AuthorityStatus { return AuthorityStatus{Text: "SSH:u@h", Connected: false} },
		NeedsPassword: func() bool { return true },
		Reconnect: func(_ context.Context, password string) error {
			got = password
			return nil
		},
	})
	if cmd := m.beginAuthorityReconnect(); cmd != nil {
		t.Fatal("an auth-needed reconnect ran without the prompt")
	}
	if !m.promptOpen() {
		t.Fatal("the SSH password prompt did not open")
	}
	cmd := m.answer(prompt.Done{Tag: authorityPasswordTag{}, Outcome: prompt.Submitted, Text: "secret"})
	if cmd == nil {
		t.Fatal("the submitted password did not reconnect")
	}
	cmd()
	if got != "secret" {
		t.Fatalf("reconnect password = %q", got)
	}
}

// TestAuthorityFocusReconnect checks the terminal focus message drives the activation
// hook through Update.
func TestAuthorityFocusReconnect(t *testing.T) {
	m := statusViewModel(t, nil)
	connected := false
	m.SetAuthorityHooks(AuthorityHooks{
		Status: func() AuthorityStatus { return AuthorityStatus{Text: "SSH:u@h", Connected: connected} },
		Reconnect: func(context.Context, string) error {
			connected = true
			return nil
		},
	})
	_, cmd := m.Update(tea.FocusMsg{})
	if cmd == nil {
		t.Fatal("FocusMsg did not trigger the reconnect command")
	}
	cmd()
	if !connected {
		t.Fatal("the reconnect did not run")
	}
}

// TestAuthorityCancelledPromptIsNotReopenedByThePoll checks Esc on the credential prompt
// is final for the poll (the next tick does not open it again) and does not wedge an
// explicit reconnect.
func TestAuthorityCancelledPromptIsNotReopenedByThePoll(t *testing.T) {
	m := statusViewModel(t, nil)
	m.SetAuthorityHooks(AuthorityHooks{
		Status:        func() AuthorityStatus { return AuthorityStatus{Text: "SSH:u@h", Connected: false} },
		NeedsPassword: func() bool { return true },
		Reconnect:     func(context.Context, string) error { return nil },
	})
	m.authorityPollTick()
	if !m.promptOpen() {
		t.Fatal("the poll did not open the SSH password prompt")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.promptOpen() {
		t.Fatal("Esc did not close the SSH password prompt")
	}
	m.authorityPollTick()
	if m.promptOpen() {
		t.Fatal("the poll reopened the cancelled SSH password prompt")
	}
	// The user can still ask again: an explicit reconnect is not blocked by the cancel.
	m.beginAuthorityReconnect()
	if !m.promptOpen() {
		t.Fatal("an explicit reconnect after Esc did not prompt")
	}
}
