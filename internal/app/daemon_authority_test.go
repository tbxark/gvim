package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/authority/authoritytest"
)

// promptFakeSlot is an authority slot that asks for the credentials of mode and records the
// answers, so the daemon start prompt loop is tested without a terminal or SSH.
type promptFakeSlot struct {
	*authoritytest.Fake
	mu         sync.Mutex
	mode       string // password, passphrase, keyboard or fail
	connected  bool
	reconnects int
	secret     string
	answers    []string
	challenge  authority.KeyboardPrompt
}

func (s *promptFakeSlot) State() authority.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return authority.State{Kind: "ssh", Label: "u@h", Connected: s.connected}
}
func (s *promptFakeSlot) Connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected
}
func (s *promptFakeSlot) Label() string { return "u@h" }
func (s *promptFakeSlot) Reconnect(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reconnects++
	if s.connected {
		return nil
	}
	if s.mode == "fail" {
		return errors.New("connection refused")
	}
	return errors.New("authentication needs input")
}
func (s *promptFakeSlot) NeedsPassword() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.connected && s.mode == "password"
}
func (s *promptFakeSlot) ReconnectWithPassword(_ context.Context, password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if password == "" {
		return errors.New("empty password")
	}
	s.secret, s.connected = password, true
	return nil
}
func (s *promptFakeSlot) NeedsPassphrase() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.connected && s.mode == "passphrase"
}
func (s *promptFakeSlot) ReconnectWithPassphrase(_ context.Context, passphrase string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.secret, s.connected = passphrase, true
	return nil
}
func (s *promptFakeSlot) NeedsKeyboardInteractive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.connected && s.mode == "keyboard"
}
func (s *promptFakeSlot) KeyboardInteractiveChallenge() (authority.KeyboardPrompt, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mode != "keyboard" {
		return authority.KeyboardPrompt{}, false
	}
	return s.challenge, true
}
func (s *promptFakeSlot) ReconnectWithAnswers(_ context.Context, answers []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.answers, s.connected = append([]string(nil), answers...), true
	return nil
}

// fakePrompter answers the daemon prompts without a terminal.
type fakePrompter struct {
	mu      sync.Mutex
	secret  string
	line    string
	secretQ []string
	lineQ   []string
	err     error
}

func (p *fakePrompter) Secret(prompt string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.secretQ = append(p.secretQ, prompt)
	return p.secret, p.err
}
func (p *fakePrompter) Line(prompt string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lineQ = append(p.lineQ, prompt)
	return p.line, p.err
}

func promptSlot(t *testing.T, mode string) *promptFakeSlot {
	t.Helper()
	slot := &promptFakeSlot{Fake: authoritytest.New(), mode: mode}
	return slot
}

// TestConnectDaemonAuthorityPrompts checks the daemon-start connect answers the authority's
// password, passphrase and 2FA requests on the prompter and reports the host in the prompt.
func TestConnectDaemonAuthorityPrompts(t *testing.T) {
	t.Run("password", func(t *testing.T) {
		slot := promptSlot(t, "password")
		p := &fakePrompter{secret: "hunter2"}
		if err := connectDaemonAuthority(context.Background(), slot, p); err != nil {
			t.Fatal(err)
		}
		if !slot.Connected() || slot.secret != "hunter2" {
			t.Fatalf("slot = connected %v secret %q", slot.Connected(), slot.secret)
		}
		if len(p.secretQ) == 0 || !strings.Contains(p.secretQ[0], "u@h") {
			t.Fatalf("password prompts = %v", p.secretQ)
		}
	})
	t.Run("passphrase", func(t *testing.T) {
		slot := promptSlot(t, "passphrase")
		p := &fakePrompter{secret: "key-pass"}
		if err := connectDaemonAuthority(context.Background(), slot, p); err != nil {
			t.Fatal(err)
		}
		if !slot.Connected() || slot.secret != "key-pass" {
			t.Fatalf("slot = connected %v secret %q", slot.Connected(), slot.secret)
		}
	})
	t.Run("keyboard", func(t *testing.T) {
		slot := promptSlot(t, "keyboard")
		slot.challenge = authority.KeyboardPrompt{Name: "2FA", Questions: []string{"code"}, Echos: []bool{false}}
		p := &fakePrompter{secret: "123456"}
		if err := connectDaemonAuthority(context.Background(), slot, p); err != nil {
			t.Fatal(err)
		}
		if !slot.Connected() || len(slot.answers) != 1 || slot.answers[0] != "123456" {
			t.Fatalf("slot = connected %v answers %v", slot.Connected(), slot.answers)
		}
	})
}

// TestConnectDaemonAuthorityFailures checks a non-interactive failure is actionable and a
// connected slot is not redialed.
func TestConnectDaemonAuthorityFailures(t *testing.T) {
	slot := promptSlot(t, "fail")
	p := &fakePrompter{}
	err := connectDaemonAuthority(context.Background(), slot, p)
	if err == nil || !strings.Contains(err.Error(), "daemon: SSH") || !strings.Contains(err.Error(), "check the SSH host/user") {
		t.Fatalf("failure = %v, want the actionable daemon error", err)
	}
	if len(p.secretQ) != 0 || len(p.lineQ) != 0 {
		t.Fatalf("a non-prompt failure asked for credentials: %v %v", p.secretQ, p.lineQ)
	}
	connected := promptSlot(t, "password")
	connected.connected = true
	if err := connectDaemonAuthority(context.Background(), connected, p); err != nil {
		t.Fatal(err)
	}
	if connected.reconnects != 0 {
		t.Fatalf("a connected slot was redialed %d times", connected.reconnects)
	}
}

// TestConnectDaemonAuthorityPromptReadError checks a terminal read failure is reported as a
// daemon error, not swallowed.
func TestConnectDaemonAuthorityPromptReadError(t *testing.T) {
	slot := promptSlot(t, "password")
	p := &fakePrompter{err: errors.New("input closed")}
	err := connectDaemonAuthority(context.Background(), slot, p)
	if err == nil || !strings.Contains(err.Error(), "reading the terminal prompt failed") {
		t.Fatalf("error = %v", err)
	}
}

// TestDaemonRemoteNonInteractiveAuthFailsBeforeHosting checks a daemon started without an
// interactive terminal fails the remote start with the actionable error instead of hosting a
// workspace it cannot reach (stdin in the test process is not a terminal).
func TestDaemonRemoteNonInteractiveAuthFailsBeforeHosting(t *testing.T) {
	isolateConfig(t)
	name := daemonTestHome(t, "app-test-remote-noauth")
	workspaces := t.TempDir()
	t.Setenv("GVIM_WORKSPACE_DIR", workspaces)
	slot := authoritytest.New()
	slot.ConnectedState = false
	slot.ReconnectErr = errors.New("authentication needs a terminal")
	slot.LabelText = "u@h"
	stubMountAuthority(t, slot)
	spec := &authority.RemoteSpec{User: "u", Host: "h", Path: "/srv/proj", Raw: "u@h:/srv/proj"}
	opts := remoteJumpBoxOptions(t, name, spec)
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), opts) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "daemon:") {
			t.Fatalf("Run error = %v, want the actionable daemon error", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the daemon did not fail the non-interactive authentication")
	}
}
