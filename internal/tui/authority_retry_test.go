package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/tui/prompt"
)

// TestAuthorityPassphrasePrompt checks an encrypted key's passphrase is asked for by the
// activation path and the submitted secret is used for the reconnect.
func TestAuthorityPassphrasePrompt(t *testing.T) {
	m := statusViewModel(t, nil)
	connected := false
	var got string
	m.SetAuthorityHooks(AuthorityHooks{
		Status:          func() AuthorityStatus { return AuthorityStatus{Text: "SSH:u@h", Connected: connected} },
		Reconnect:       func(context.Context, string) error { return nil },
		NeedsPassphrase: func() bool { return true },
		ReconnectWithPassphrase: func(_ context.Context, phrase string) error {
			got = phrase
			connected = true
			return nil
		},
	})
	if cmd := m.beginAuthorityReconnect(); cmd != nil {
		t.Fatal("a passphrase reconnect ran without the prompt")
	}
	if !m.promptOpen() {
		t.Fatal("the key passphrase prompt did not open")
	}
	cmd := m.answer(prompt.Done{Tag: authorityPassphraseTag{}, Outcome: prompt.Submitted, Text: "sekret"})
	if cmd == nil {
		t.Fatal("the submitted passphrase did not reconnect")
	}
	msg, ok := cmd().(authorityReconnectedMsg)
	if !ok || msg.err != nil {
		t.Fatalf("reconnect result = %#v", msg)
	}
	m.Update(msg)
	if got != "sekret" {
		t.Fatalf("passphrase = %q", got)
	}
}

// TestAuthorityKeyboardInteractivePromptsEachQuestion checks a 2FA challenge asks every
// question and reconnects with all the answers in order.
func TestAuthorityKeyboardInteractivePromptsEachQuestion(t *testing.T) {
	m := statusViewModel(t, nil)
	var got []string
	m.SetAuthorityHooks(AuthorityHooks{
		Status:    func() AuthorityStatus { return AuthorityStatus{Text: "SSH:u@h", Connected: false} },
		Reconnect: func(context.Context, string) error { return nil },
		KeyboardPrompt: func() ([]string, []bool, bool) {
			return []string{"Code:", "Backup code:"}, []bool{false, false}, true
		},
		ReconnectWithAnswers: func(_ context.Context, answers []string) error {
			got = append([]string(nil), answers...)
			return nil
		},
	})
	if cmd := m.beginAuthorityReconnect(); cmd != nil {
		t.Fatal("a keyboard-interactive reconnect ran without asking")
	}
	if !m.promptOpen() {
		t.Fatal("the first 2FA question did not open")
	}
	if cmd := m.answer(prompt.Done{Tag: authorityChallengeTag{}, Outcome: prompt.Submitted, Text: "111111"}); cmd != nil {
		t.Fatal("the reconnect ran before the second question")
	}
	if !m.promptOpen() {
		t.Fatal("the second 2FA question did not open")
	}
	cmd := m.answer(prompt.Done{Tag: authorityChallengeTag{}, Outcome: prompt.Submitted, Text: "222222"})
	if cmd == nil {
		t.Fatal("the last answer did not reconnect")
	}
	msg := cmd().(authorityReconnectedMsg)
	if msg.err != nil {
		t.Fatalf("reconnect error = %v", msg.err)
	}
	m.Update(msg)
	if len(got) != 2 || got[0] != "111111" || got[1] != "222222" {
		t.Fatalf("answers = %v, want both in order", got)
	}
}

// TestAuthorityBackgroundReconnectRetries checks the poll schedules a reconnect in a
// command with capped backoff, and one dial runs at a time.
func TestAuthorityBackgroundReconnectRetries(t *testing.T) {
	oldPoll, oldMin, oldMax := authorityPollEvery, authorityRetryMin, authorityRetryMax
	authorityPollEvery, authorityRetryMin, authorityRetryMax = time.Millisecond, time.Millisecond, 4*time.Millisecond
	t.Cleanup(func() { authorityPollEvery, authorityRetryMin, authorityRetryMax = oldPoll, oldMin, oldMax })

	m := statusViewModel(t, nil)
	connected := false
	reconnects := 0
	m.SetAuthorityHooks(AuthorityHooks{
		Status: func() AuthorityStatus {
			return AuthorityStatus{Text: "SSH:u@h", Connected: connected}
		},
		Reconnect: func(context.Context, string) error {
			reconnects++
			return errors.New("still down")
		},
	})
	if cmd := m.authorityPollCmd(); cmd == nil {
		t.Fatal("a remote authority got no background poll")
	}
	if cmd := m.authorityPollTick(); cmd == nil {
		t.Fatal("a disconnected authority did not schedule a retry")
	}
	if !m.authorityBusy {
		t.Fatal("the retry is not marked in flight")
	}
	// A second poll while the retry is pending must not start another dial.
	if cmd := m.beginAuthorityReconnect(); cmd != nil {
		t.Fatal("activation started a second reconnect while one was in flight")
	}
	cmd := m.authorityRetryTick()
	if cmd == nil {
		t.Fatal("the due retry produced no command")
	}
	msg, ok := cmd().(authorityReconnectedMsg)
	if !ok || msg.err == nil {
		t.Fatalf("retry result = %#v, want the failure", msg)
	}
	m.Update(msg)
	if reconnects != 1 {
		t.Fatalf("reconnects = %d, want one", reconnects)
	}
	if m.authorityBackoff == 0 {
		t.Fatal("a failed retry did not keep a backoff")
	}
	// A successful dial clears the backoff and the busy mark.
	connected = true
	m.Update(authorityReconnectedMsg{})
	if m.authorityBusy || m.authorityBackoff != 0 {
		t.Fatalf("success left busy=%v backoff=%v", m.authorityBusy, m.authorityBackoff)
	}
}

// TestAuthorityConnectingStatus checks the status row says reconnecting while a dial runs.
func TestAuthorityConnectingStatus(t *testing.T) {
	m := statusViewModel(t, nil)
	m.SetAuthorityHooks(AuthorityHooks{Status: func() AuthorityStatus {
		return AuthorityStatus{Text: "SSH:u@h", Connecting: true}
	}})
	if row := statusRowText(m); !strings.Contains(row, "SSH:u@h · reconnecting") {
		t.Fatalf("status row %q has no reconnecting indicator", row)
	}
}

// TestAuthorityRetryTickInvalidatesPreviewsBeforeDial: the background retry tick is an
// attempt boundary like the focus path: it clears cached palette previews and marks the
// carrier disconnected before the dial command runs.
func TestAuthorityRetryTickInvalidatesPreviewsBeforeDial(t *testing.T) {
	m := newTestModel(t, newFakeEditor("x"), nil, 80, 24)
	o := &invalidateOverlay{fakeOverlay: fakeOverlay{open: true}}
	m.pushOverlay(o)
	connected := true
	dials := 0
	m.SetAuthorityHooks(AuthorityHooks{
		Status: func() AuthorityStatus { return AuthorityStatus{Text: "SSH:h", Connected: connected} },
		Reconnect: func(context.Context, string) error {
			dials++
			return errors.New("still down")
		},
	})
	if !m.authorityPreviewConnected {
		t.Fatal("hooks did not seed the connected preview state")
	}
	connected = false
	cmd := m.authorityRetryTick()
	if cmd == nil {
		t.Fatal("the retry tick produced no dial command")
	}
	if o.calls != 1 {
		t.Fatalf("retry tick invalidated previews %d times, want 1 before the dial", o.calls)
	}
	if m.authorityPreviewConnected {
		t.Fatal("retry tick left the preview state connected")
	}
	if dials != 0 {
		t.Fatal("the dial ran inside Update before its command was executed")
	}
	_ = cmd() // execute the dial only after the state boundary was checked
}
