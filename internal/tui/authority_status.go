package tui

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/prompt"
)

// AuthorityStatus is the workspace-authority snapshot the status row and the
// reconnect-on-activate path read: the label text without brackets ("SSH:user@host"),
// whether the backend is connected, and why it is not.
type AuthorityStatus struct {
	Text      string
	Connected bool
	Reason    string
}

// AuthorityHooks are the app's authority calls. They are optional: without them the
// status row shows no segment and activation reconnects nothing.
type AuthorityHooks struct {
	// Status returns the current snapshot; nil means no authority segment.
	Status func() AuthorityStatus
	// NeedsPassword reports whether reconnecting should ask for a password first.
	NeedsPassword func() bool
	// Reconnect blocks until the backend reconnected; password "" means no prompt.
	Reconnect func(ctx context.Context, password string) error
}

// SetAuthorityHooks gives the root the workspace-authority hooks. The app calls it once,
// after New, for any launch (local hooks show nothing and reconnect nothing).
func (m *Model) SetAuthorityHooks(h AuthorityHooks) { m.auth = h }

// OpenAuthorityTerminalAtStart opens the dock terminal in Init: a remote launch starts
// the SSH terminal at the remote workspace instead of a local file.
func (m *Model) OpenAuthorityTerminalAtStart() { m.authOpenDock = true }

// authoritySegment returns the status-row item of the authority, if it has one.
func (m *Model) authoritySegment() (statusItem, bool) {
	if m.auth.Status == nil {
		return statusItem{}, false
	}
	st := m.auth.Status()
	if st.Text == "" {
		return statusItem{}, false
	}
	text := st.Text
	if !st.Connected {
		text += " · disconnected"
	}
	return statusItem{ID: "authority", Text: text, Prio: 1 << 20}, true
}

// authorityInitCmd opens the dock terminal of a remote launch once the program starts.
func (m *Model) authorityInitCmd() tea.Cmd {
	if !m.authOpenDock {
		return nil
	}
	m.authOpenDock = false
	return m.showDock()
}

// beginAuthorityReconnect is the activation hook: a disconnected remote authority
// reconnects, asking for a password first when the last attempt failed on credentials.
func (m *Model) beginAuthorityReconnect() tea.Cmd {
	if m.auth.Status == nil || m.auth.Reconnect == nil {
		return nil
	}
	if m.auth.Status().Connected {
		return nil
	}
	if m.auth.NeedsPassword != nil && m.auth.NeedsPassword() {
		m.pushOverlay(prompt.NewLine(m.styles, prompt.LineOptions{
			Label: "SSH password:",
			Tag:   authorityPasswordTag{},
		}))
		return nil
	}
	return m.authorityReconnectCmd("")
}

// authorityReconnectCmd runs one reconnect off the UI goroutine.
func (m *Model) authorityReconnectCmd(password string) tea.Cmd {
	fn := m.auth.Reconnect
	if fn == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return authorityReconnectedMsg{err: fn(ctx, password)}
	}
}

// authorityReconnectedMsg is the outcome of a reconnect.
type authorityReconnectedMsg struct{ err error }

// authorityPasswordTag is the tag of the SSH password prompt.
type authorityPasswordTag struct{}

// reportAuthorityReconnect puts the outcome of a reconnect on the status row. The
// segment itself is read while the row is built, so no further state is kept here.
func (m *Model) reportAuthorityReconnect(err error) {
	if err != nil {
		m.quietNotice("SSH reconnect failed: " + err.Error())
		return
	}
	m.quietNotice("SSH reconnected")
}

// SetShellRunner routes shell_command and format_buffer through fn instead of
// editor.RunShellJob. The app gives the workspace authority's one-shot as fn, so the
// command runs on the backend the workspace lives on. Nil keeps the local runner.
func (m *Model) SetShellRunner(fn func(editor.ShellJob) (string, error)) { m.shellRun = fn }
