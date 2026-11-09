package tui

import (
	"context"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/internal/tui/popup"
)

// TestRemoteMenuLocal checks the local workspace menu: it names the local workspace
// and says there is no remote authority in this session, rather than claiming remote
// workspaces are unsupported.
func TestRemoteMenuLocal(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, testKeys(), Settings{}, nil)
	if err := m.Run("show_remote_indicator_menu"); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if len(m.overlays) != 1 {
		t.Fatalf("overlays = %d, want the menu", len(m.overlays))
	}
	if !frameHas(m, "Remote") || !frameRowHas(m, "Workspace", "Local") {
		t.Fatal("the menu does not say the workspace is local")
	}
	if !frameHas(m, "No remote authority in this session") || !frameHas(m, "start gvim with an SSH workspace") {
		t.Fatal("the local menu does not explain that this session has no remote authority")
	}
	if frameHas(m, "not supported") {
		t.Fatal("the local menu still claims remote workspaces are unsupported")
	}
}

// TestRemoteMenuShowsAuthorityStateAndReconnect checks a remote workspace: the menu
// reports the SSH user@host, its disconnected state and reason, the remote root, and
// offers the existing reconnect action, which runs off the update goroutine.
func TestRemoteMenuShowsAuthorityStateAndReconnect(t *testing.T) {
	ed := newFakeEditor("hello")
	m := newUtilityModel(t, ed, testKeys(), Settings{}, nil)
	m.fileRoot = "/remote/w"
	connected := false
	reconnected := false
	m.SetAuthorityHooks(AuthorityHooks{
		Status: func() AuthorityStatus {
			return AuthorityStatus{Text: "SSH:u@h", Connected: connected, Reason: "dial tcp: connection refused"}
		},
		Reconnect: func(context.Context, string) error {
			reconnected = true
			return nil
		},
	})
	if err := m.Run("show_remote_indicator_menu"); err != nil {
		t.Fatal(err)
	}
	if !frameHas(m, "Workspace: Remote") || !frameHas(m, "u@h") {
		t.Fatal("the remote menu does not show the SSH authority")
	}
	if !frameHas(m, "Disconnected") || !frameHas(m, "dial tcp: connection refused") {
		t.Fatal("the remote menu does not show the disconnected state and reason")
	}
	if !frameHas(m, "Remote root") || !frameHas(m, "/remote/w") {
		t.Fatal("the remote menu does not show the remote workspace root")
	}
	if !frameHas(m, "Reconnect SSH") {
		t.Fatal("the remote menu has no reconnect action")
	}
	_, cmd := m.Update(popup.Chosen{Item: popup.Item{Data: remoteChoice{action: "reconnect"}}})
	if cmd == nil {
		t.Fatal("choosing Reconnect SSH returned no command")
	}
	if reconnected {
		t.Fatal("reconnect ran synchronously in Update")
	}
	_, _ = m.Update(cmd())
	if !reconnected {
		t.Fatal("the reconnect command did not run the authority reconnect")
	}

	// A connected authority still reports its state; the action says it is connected
	// instead of starting a second reconnect.
	connected = true
	m2 := newUtilityModel(t, newFakeEditor("hello"), testKeys(), Settings{}, nil)
	m2.fileRoot = "/remote/w"
	m2.SetAuthorityHooks(AuthorityHooks{
		Status:    func() AuthorityStatus { return AuthorityStatus{Text: "SSH:u@h", Connected: true} },
		Reconnect: func(context.Context, string) error { t.Fatal("reconnect ran while connected"); return nil },
	})
	runAction(t, m2, "show_remote_indicator_menu")
	if !frameHas(m2, "Connected") || !frameHas(m2, "Workspace: Remote") {
		t.Fatal("the connected remote menu does not show its state")
	}
	_, cmd = m2.Update(popup.Chosen{Item: popup.Item{Data: remoteChoice{action: "reconnect"}}})
	if cmd != nil {
		t.Fatal("choosing reconnect while connected returned a command")
	}
	if !strings.Contains(m2.notice, "Already connected") {
		t.Fatalf("notice = %q, want Already connected", m2.notice)
	}
}

func TestRemoteMenuEnterAndEscClose(t *testing.T) {
	for _, k := range []tea.KeyPressMsg{special(tea.KeyEnter), special(tea.KeyEscape)} {
		ed := newFakeEditor("hello")
		m := newUtilityModel(t, ed, testKeys(), Settings{}, nil)
		runAction(t, m, "show_remote_indicator_menu")
		press(m, k)
		if len(m.overlays) != 0 {
			t.Fatalf("key %v left %d overlays open", k, len(m.overlays))
		}
	}
}

// TestRemoteMenuSanitizesAuthorityFields checks the untrusted authority fields: the
// SSH label, the disconnected reason and the remote root all pass through textsafe
// (invalid UTF-8, Cc and Cf removed; tab/newline flattened) before the popup paints
// them, so a hostile host cannot inject terminal escapes, bidi overrides or extra
// rows.
func TestRemoteMenuSanitizesAuthorityFields(t *testing.T) {
	t.Parallel()
	m := newUtilityModel(t, newFakeEditor("hello"), testKeys(), Settings{}, nil)
	m.fileRoot = "/remote/\x1b]0;title\x07w\nroot\xff"
	m.SetAuthorityHooks(AuthorityHooks{
		Status: func() AuthorityStatus {
			return AuthorityStatus{
				Text:   "SSH:u@h\x1b[31m\x07\nadmin\u202e",
				Reason: "refused\x00\r\x1b[2Jretry",
			}
		},
		Reconnect: func(context.Context, string) error { return nil },
	})
	if err := m.Run("show_remote_indicator_menu"); err != nil {
		t.Fatal(err)
	}
	text := strings.Join(frameText(m), "\n")
	for _, r := range text {
		if r == '\n' || r == '\t' {
			continue
		}
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) {
			t.Fatalf("the menu painted a control/format rune %U in %q", r, text)
		}
	}
	if !utf8.ValidString(text) {
		t.Fatal("the menu painted invalid UTF-8")
	}
	for _, want := range []string{"u@h", "refused", "/remote/"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the sanitized menu lost %q: %q", want, text)
		}
	}
	for _, bad := range []string{"\x1b", "\x00", "\x07", "\r", "\u202e"} {
		if strings.Contains(text, bad) {
			t.Fatalf("the menu painted hostile sequence %q", bad)
		}
	}
}

// TestRemoteReconnectConnectingAndBusy checks the reconnect row distinguishes an
// in-flight connection (Connecting or a busy authority) from an already connected
// workspace: it reports the state instead of dialing again or saying "Already
// connected".
func TestRemoteReconnectConnectingAndBusy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		connected  bool
		connecting bool
		busy       bool
		want       string
	}{
		{name: "connecting", connecting: true, want: "Reconnect already in progress"},
		{name: "busy", busy: true, want: "Reconnect already in progress"},
		{name: "connected", connected: true, want: "Already connected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := newUtilityModel(t, newFakeEditor("hello"), testKeys(), Settings{}, nil)
			m.fileRoot = "/remote/w"
			called := false
			m.SetAuthorityHooks(AuthorityHooks{
				Status: func() AuthorityStatus {
					return AuthorityStatus{Text: "SSH:u@h", Connected: tc.connected, Connecting: tc.connecting}
				},
				Reconnect: func(context.Context, string) error { called = true; return nil },
			})
			m.authorityBusy = tc.busy
			runAction(t, m, "show_remote_indicator_menu")
			_, cmd := m.Update(popup.Chosen{Item: popup.Item{Data: remoteChoice{action: "reconnect"}}})
			if cmd != nil {
				t.Fatal("a reconnect requested a dial in a connecting/busy/connected state")
			}
			if called {
				t.Fatal("the reconnect hook ran in a connecting/busy/connected state")
			}
			if !strings.Contains(m.notice, tc.want) {
				t.Fatalf("notice = %q, want %q", m.notice, tc.want)
			}
		})
	}
}

// TestF6RemoteMenuKeyboardReconnect checks the real key path through the popup: F6,
// Down to the Reconnect row, Enter, then the popup's Chosen message starts the
// reconnect in a command (not in Update).
func TestF6RemoteMenuKeyboardReconnect(t *testing.T) {
	t.Parallel()
	m := newTOCModel(t, newTOCEditor("notes.md", "# One"), nil)
	m.fileRoot = "/remote/w"
	reconnected := false
	m.SetAuthorityHooks(AuthorityHooks{
		Status: func() AuthorityStatus {
			return AuthorityStatus{Text: "SSH:u@h", Connected: false, Reason: "dial refused"}
		},
		Reconnect: func(context.Context, string) error { reconnected = true; return nil },
	})
	press(m, special(tea.KeyF6))
	if !frameHas(m, "Reconnect SSH") {
		t.Fatal("the remote menu has no Reconnect SSH row")
	}
	for range 3 { // Workspace, state, remote root, then Reconnect
		press(m, special(tea.KeyDown))
	}
	enter := press(m, special(tea.KeyEnter))
	if enter == nil {
		t.Fatal("Enter on the Reconnect row returned no command")
	}
	msg := enter()
	chosen, ok := msg.(popup.Chosen)
	if !ok {
		t.Fatalf("Enter produced %T, want popup.Chosen", msg)
	}
	if choice, ok := chosen.Item.Data.(remoteChoice); !ok || choice.action != "reconnect" {
		t.Fatalf("chosen row = %#v, want the Reconnect action", chosen.Item)
	}
	_, cmd := m.Update(chosen)
	if cmd == nil {
		t.Fatal("choosing Reconnect returned no reconnect command")
	}
	if reconnected {
		t.Fatal("the reconnect ran synchronously in Update")
	}
	_, _ = m.Update(cmd())
	if !reconnected {
		t.Fatal("the reconnect command did not run the authority reconnect")
	}
}

// TestRemoteMenuFromOverlayContexts checks the contextual keymap path: a prompt and a
// popup resolve their own keymap context first, so a user binding (or Fresh's global
// fallback in the real map) can run show_remote_indicator_menu there. The menu opens
// over the overlay and closing it returns to the text/list instead of eating a typed
// character.
func TestRemoteMenuFromOverlayContexts(t *testing.T) {
	t.Parallel()
	bindings := fakeKeymap{
		"prompt|F6|": {Action: "show_remote_indicator_menu"},
		"popup|F6|":  {Action: "show_remote_indicator_menu"},
	}
	m := overlayModel(t, bindings)
	p := pushPrompt(m, "abc")
	press(m, special(tea.KeyF6))
	if len(m.overlays) != 2 || !frameHas(m, "Remote") {
		t.Fatalf("F6 from a prompt did not open the Remote Indicator menu over it (overlays %d)", len(m.overlays))
	}
	press(m, special(tea.KeyEscape))
	if len(m.overlays) != 1 {
		t.Fatalf("closing the remote menu left %d overlays, want the prompt", len(m.overlays))
	}
	// The text field still receives typed characters afterwards.
	press(m, typed("x"))
	if got := p.Value(); !strings.Contains(got, "abc") {
		t.Fatalf("prompt value after the menu = %q, want the text preserved", got)
	}

	m = overlayModel(t, bindings)
	m.pushOverlay(list.New("Pick", []list.Row{{Left: "a", Value: "a"}}, m.styles.Overlay, nil))
	press(m, special(tea.KeyF6))
	if len(m.overlays) != 2 || !frameHas(m, "Remote") {
		t.Fatalf("F6 from a popup did not open the Remote Indicator menu over it (overlays %d)", len(m.overlays))
	}
	press(m, special(tea.KeyEscape))
	if len(m.overlays) != 1 {
		t.Fatalf("closing the remote menu left %d popup overlays", len(m.overlays))
	}
}
