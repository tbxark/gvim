package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/trust"
)

// The workspace-trust texts, Fresh's locale strings. The command names and the
// level labels come from here; the dialog's own copy is in trustdialog.go.
const (
	// trustUnavailableNotice is Fresh's trust.unavailable: the session could not
	// build a trust store, so there is no decision to show or take.
	trustUnavailableNotice = "Workspace trust is unavailable in this workspace"
	// trustTrustedNotice, trustRestrictedNotice and trustBlockedNotice are
	// Fresh's trust.now_* messages, shown when a decision is recorded.
	trustTrustedNotice    = "Workspace trusted — project tooling may run processes"
	trustRestrictedNotice = "Workspace restricted — repo-controlled execution is blocked"
	trustBlockedNotice    = "Workspace blocked — no processes will run"
)

// trustDecidedMsg is the result of the command that records a trust decision:
// the state after it, and why the decision was not saved when it was not.
type trustDecidedMsg struct {
	state trust.State
	err   error
}

// trustLoadedMsg is the trust state of a workspace that was just opened, with
// the startup policy applied.
type trustLoadedMsg struct {
	state trust.State
	err   error
}

// startTrustPrompt opens the mandatory open-time trust prompt when the open
// workspace is undecided and can execute code. The app's startup policy leaves
// such a workspace Restricted and undecided, so the decision is the first thing
// the editor asks.
func (m *Model) startTrustPrompt() {
	if !m.trust.available() || !m.trust.state.NeedsPrompt() {
		return
	}
	m.openTrustDialog(false)
}

// openWorkspaceTrust opens the workspace-trust dialog: the Workspace Trust…
// command and a click on the status-bar pill. The prompt is cancellable: the
// user may look at the level and leave it as it is. A session without a trust
// port says why it cannot open one.
func (m *Model) openWorkspaceTrust() tea.Cmd {
	if !m.trust.available() {
		m.notice = trustUnavailableNotice
		return nil
	}
	m.openTrustDialog(true)
	return nil
}

// openTrustDialog pushes the trust prompt. cancellable picks the secondary
// button: Cancel for a prompt the user opened, Quit for the mandatory open-time
// prompt, where there is nothing to fall back to before a decision. A prompt
// that is already on the stack is not stacked twice.
func (m *Model) openTrustDialog(cancellable bool) {
	if m.trustPromptOpen() {
		return
	}
	m.pushOverlay(newTrustDialog(m, cancellable))
}

// trustPromptOpen reports whether the trust prompt is on the overlay stack.
func (m *Model) trustPromptOpen() bool {
	return slices.ContainsFunc(m.overlays, func(o Overlay) bool {
		_, ok := o.(*trustDialog)
		return ok
	})
}

// loadTrust returns the command that loads the trust state of root, for a
// project that was just opened. It reads the store and the folder, so it runs
// in a command, never in Update.
func (m *Model) loadTrust(root string) tea.Cmd {
	if !m.trust.available() {
		return nil
	}
	port := m.trust.port
	return func() tea.Msg {
		state, err := port.Load(root)
		return trustLoadedMsg{state: state, err: err}
	}
}

// decideTrust returns the command that records level for the open workspace.
// The port writes the store, so the write runs in a command.
func decideTrust(port TrustPort, level trust.Level) tea.Cmd {
	return func() tea.Msg {
		state, err := port.Decide(level)
		return trustDecidedMsg{state: state, err: err}
	}
}

// trustMessage handles the messages of the workspace-trust feature. It reports
// whether it took msg, and the command the message starts.
func (m *Model) trustMessage(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case trustDecidedMsg:
		return m.trustDecided(msg), true
	case trustLoadedMsg:
		return m.trustLoaded(msg), true
	}
	return nil, false
}

// trustDecided applies the state a decision left behind, and reports on the
// status row what the decision means. A decision that could not be persisted
// still applies to the session, and the row says it will not be remembered.
func (m *Model) trustDecided(msg trustDecidedMsg) tea.Cmd {
	if msg.state.Root != "" {
		m.trust.state = msg.state
		m.emitTrustChanged(string(msg.state.Level))
	}
	if msg.err != nil {
		m.notice = "Workspace trust was not saved: " + msg.err.Error()
		return nil
	}
	m.notice = trustNotice(msg.state.Level)
	return nil
}

// trustLoaded applies the trust state of a workspace that was just opened. A
// workspace that is undecided and can execute code opens the cancellable
// prompt, as Fresh does when another workspace is activated. A failed load is
// a notice, and the previous state stays.
func (m *Model) trustLoaded(msg trustLoadedMsg) tea.Cmd {
	if msg.state.Root == "" {
		if msg.err != nil {
			m.notice = "Workspace trust: " + msg.err.Error()
		}
		return nil
	}
	m.trust.state = msg.state
	m.emitTrustChanged(string(msg.state.Level))
	if msg.err != nil {
		m.notice = "Workspace trust was not saved: " + msg.err.Error()
		return nil
	}
	if m.trust.state.NeedsPrompt() {
		m.openTrustDialog(true)
	}
	return nil
}

// trustNotice returns the status-row message for a recorded level, Fresh's
// trust.now_* text.
func trustNotice(level trust.Level) string {
	switch level {
	case trust.Trusted:
		return trustTrustedNotice
	case trust.Blocked:
		return trustBlockedNotice
	default:
		return trustRestrictedNotice
	}
}
