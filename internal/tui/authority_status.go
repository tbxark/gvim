package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/i18n"
	"github.com/tbxark/gvim/internal/tui/prompt"
	"github.com/tbxark/gvim/internal/tui/textsafe"
)

// AuthorityStatus is the workspace-authority snapshot the status row and the
// reconnect-on-activate path read: the label text without brackets ("SSH:user@host"),
// whether the backend is connected, and why it is not.
type AuthorityStatus struct {
	Text      string
	Connected bool
	Reason    string
	// Connecting says a reconnect is dialing right now.
	Connecting bool
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
	// NeedsPassphrase reports whether the next reconnect needs an encrypted identity's
	// key passphrase; ReconnectWithPassphrase redials with it. Nil skips the prompt.
	NeedsPassphrase         func() bool
	ReconnectWithPassphrase func(ctx context.Context, passphrase string) error
	// KeyboardPrompt returns a pending keyboard-interactive challenge (2FA): its
	// questions and whether each answer is echoed. ReconnectWithAnswers redials with
	// the answers. Nil skips the challenge path.
	KeyboardPrompt       func() (questions []string, echos []bool, ok bool)
	ReconnectWithAnswers func(ctx context.Context, answers []string) error
	// AfterReconnect, when set, runs once after a successful reconnect, off the update
	// goroutine, and reports how many language servers were restarted (or why not). The
	// app uses it for LSP restart on a remote workspace; nil does nothing.
	AfterReconnect func() (int, error)
}

// SetAuthorityHooks gives the root the workspace-authority hooks. The app calls it once,
// after New, for any launch (local hooks show nothing and reconnect nothing).
func (m *Model) SetAuthorityHooks(h AuthorityHooks) {
	m.auth = h
	// Seed the preview-cache connectivity state now: the first poll may not run before
	// the user opens the palette, and a disconnected carrier must not inherit a stale
	// "connected" assumption.
	m.authorityPreviewConnected = h.Status != nil && h.Status().Connected
}

// invalidateRemotePreviews drops cached remote preview reads when the authority's
// connectivity changes. It runs on the update loop; only in-memory state is touched.
func (m *Model) invalidateRemotePreviews() {
	for _, o := range m.overlays {
		if inv, ok := o.(interface{ InvalidatePreviews() }); ok {
			inv.InvalidatePreviews()
		}
	}
}

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
	// The remote SSH label carries the host separately: the status row renders the
	// catalogue key for the connected/disconnected state with %{userhost}, so a
	// translated catalogue changes the wording while the English fallback keeps
	// today's text. A label that is not an SSH authority keeps its own words.
	// The authority label comes from the remote spec (user input/config): clean it
	// before the status row paints it, as the preview surfaces do.
	text := textsafe.Clean(st.Text)
	if st.Connecting {
		return statusItem{ID: "authority", Text: text + " · reconnecting", Prio: 1 << 20}, true
	}
	if userhost, ok := strings.CutPrefix(text, "SSH:"); ok {
		key := "gvim.status.remote_ssh"
		if !st.Connected {
			key = "gvim.status.remote_ssh_disconnected"
		}
		text = i18n.T("", key, map[string]any{"userhost": userhost})
	} else if !st.Connected {
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
	if m.auth.Status().Connected || m.authorityBusy {
		return nil
	}
	// The attempt starts before any credential prompt or dial: drop old previews now.
	m.noteAuthorityReconnectAttempt()
	if m.auth.NeedsPassword != nil && m.auth.NeedsPassword() {
		m.authorityBusy = true
		m.pushOverlay(prompt.NewLine(m.styles, prompt.LineOptions{
			Label: uiText("gvim.prompt.ssh_password", "SSH password:"),
			Tag:   authorityPasswordTag{},
		}))
		return nil
	}
	if m.auth.NeedsPassphrase != nil && m.auth.NeedsPassphrase() {
		m.authorityBusy = true
		m.pushOverlay(prompt.NewLine(m.styles, prompt.LineOptions{
			Label: uiText("gvim.prompt.ssh_key_passphrase", "SSH key passphrase:"),
			Tag:   authorityPassphraseTag{},
		}))
		return nil
	}
	if m.auth.KeyboardPrompt != nil {
		if questions, echos, ok := m.auth.KeyboardPrompt(); ok && len(questions) > 0 {
			m.authorityBusy = true
			m.authorityAnswers = nil
			m.authorityQuestions = append([]string(nil), questions...)
			m.authorityEchos = append([]bool(nil), echos...)
			return m.askAuthorityChallenge()
		}
	}
	return m.authorityReconnectCmd("")
}

// noteAuthorityReconnectAttempt starts a reconnect attempt: cached remote previews are
// invalidated immediately (not only after the dial fails) and the tracker is marked
// disconnected, so Update never paints an old-carrier preview during the attempt.
func (m *Model) noteAuthorityReconnectAttempt() {
	m.authorityPreviewConnected = false
	m.invalidateRemotePreviews()
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

// authorityRestartDoneMsg is the outcome of the app's AfterReconnect work.
type authorityRestartDoneMsg struct {
	count int
	err   error
}

// reportAuthorityRestart puts the post-reconnect work's outcome on the status row: a
// failure is a notice, and a successful restart says how many servers came back.
func (m *Model) reportAuthorityRestart(count int, err error) {
	if m.authorityRefreshNotice {
		m.authorityRefreshNotice = false
		notice := uiText("gvim.notice.ssh_reconnected", "SSH reconnected: remote workspace refreshed")
		switch {
		case err != nil:
			notice += uiTextVars("gvim.notice.post_reconnect_refresh_failed", "; post-reconnect refresh failed: %{error}", map[string]any{"error": err.Error()})
		case count > 0:
			notice += i18n.TN("", "gvim.notice.language_servers_restarted", count, nil)
		}
		m.quietNotice(notice)
		return
	}
	if err != nil {
		m.quietNotice(uiTextVars("gvim.notice.lsp_restart_failed", "LSP restart after reconnect failed: %{error}", map[string]any{"error": err.Error()}))
		return
	}
	if count > 0 {
		m.quietNotice(i18n.TN("", "gvim.notice.lsp_restarted_after_reconnect", count, nil))
	}
}

// authorityPasswordTag is the tag of the SSH password prompt.
type authorityPasswordTag struct{}

// authorityPassphraseTag is the tag of the encrypted SSH key passphrase prompt.
type authorityPassphraseTag struct{}

// authorityChallengeTag is the tag of one keyboard-interactive (2FA) question.
type authorityChallengeTag struct{}

// Background reconnect: while a remote authority is disconnected the root polls the
// snapshot and retries in commands with capped backoff, so a keepalive-detected drop
// heals without the next user operation hanging. The activation path stays available and
// both paths share authorityBusy, so two reconnects never run at once.
var (
	authorityPollEvery = 3 * time.Second
	authorityRetryMin  = time.Second
	authorityRetryMax  = 30 * time.Second
)

// authorityPollTickMsg is one poll of the authority snapshot.
type authorityPollTickMsg struct{}

// authorityRetryTickMsg is one due background reconnect.
type authorityRetryTickMsg struct{}

// authorityPollCmd starts the status poll when the app wired a remote authority.
func (m *Model) authorityPollCmd() tea.Cmd {
	if m.auth.Status == nil || m.auth.Reconnect == nil {
		return nil
	}
	return m.authorityPollAfter(authorityPollEvery)
}

// authorityPollAfter arms one poll.
func (m *Model) authorityPollAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return authorityPollTickMsg{} })
}

// authorityPollTick looks at the snapshot and schedules either the next poll, the next
// credential prompt once, or a backoff retry. It never dials on the update loop.
func (m *Model) authorityPollTick() tea.Cmd {
	next := m.authorityPollAfter(authorityPollEvery)
	if m.auth.Status == nil || m.auth.Reconnect == nil {
		return nil
	}
	st := m.auth.Status()
	if st.Connected {
		m.authorityPreviewConnected = true
		m.resetAuthorityRetry()
		return next
	}
	if m.authorityPreviewConnected {
		m.authorityPreviewConnected = false
		m.invalidateRemotePreviews()
	}
	if st.Connecting || m.authorityBusy {
		return next
	}
	if m.authorityNeedsPrompt() {
		if m.authorityPrompted {
			return next
		}
		m.authorityPrompted = true
		if cmd := m.beginAuthorityReconnect(); cmd != nil {
			m.authorityBusy = true
			return tea.Batch(next, cmd)
		}
		return next
	}
	delay := m.authorityBackoff
	if delay <= 0 {
		delay = authorityRetryMin
	}
	m.authorityBusy = true
	m.authorityBackoff = min(delay*2, authorityRetryMax)
	return tea.Batch(next, tea.Tick(delay, func(time.Time) tea.Msg { return authorityRetryTickMsg{} }))
}

// authorityRetryTick runs the due reconnect in a command, unless the carrier came back.
func (m *Model) authorityRetryTick() tea.Cmd {
	if m.auth.Status == nil || m.auth.Reconnect == nil {
		return nil
	}
	if m.auth.Status().Connected {
		m.resetAuthorityRetry()
		return nil
	}
	m.noteAuthorityReconnectAttempt()
	return m.authorityReconnectCmd("")
}

// authorityNeedsPrompt reports whether the next dial can only proceed with UI input.
func (m *Model) authorityNeedsPrompt() bool {
	if m.auth.NeedsPassword != nil && m.auth.NeedsPassword() {
		return true
	}
	if m.auth.NeedsPassphrase != nil && m.auth.NeedsPassphrase() {
		return true
	}
	if m.auth.KeyboardPrompt != nil {
		if _, _, ok := m.auth.KeyboardPrompt(); ok {
			return true
		}
	}
	return false
}

// resetAuthorityRetry clears the backoff and the one-shot prompt flags after a success.
func (m *Model) resetAuthorityRetry() {
	m.authorityBusy = false
	m.authorityBackoff = 0
	m.authorityPrompted = false
	m.authorityQuestions = nil
	m.authorityAnswers = nil
	m.authorityEchos = nil
}

// askAuthorityChallenge opens the prompt of the next keyboard-interactive question, or
// dials with all the answers once there is none.
func (m *Model) askAuthorityChallenge() tea.Cmd {
	if len(m.authorityQuestions) == 0 {
		answers := m.authorityAnswers
		m.authorityAnswers = nil
		return m.authorityAnswersCmd(answers)
	}
	question := m.authorityQuestions[0]
	m.authorityQuestions = m.authorityQuestions[1:]
	if len(m.authorityEchos) > 0 {
		m.authorityEchos = m.authorityEchos[1:]
	}
	m.pushOverlay(prompt.NewLine(m.styles, prompt.LineOptions{
		Label: question,
		Tag:   authorityChallengeTag{},
	}))
	return nil
}

// authorityPassphraseCmd redials with the submitted passphrase.
func (m *Model) authorityPassphraseCmd(passphrase string) tea.Cmd {
	fn := m.auth.ReconnectWithPassphrase
	if fn == nil {
		m.resetAuthorityRetry()
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return authorityReconnectedMsg{err: fn(ctx, passphrase)}
	}
}

// authorityAnswersCmd redials with the submitted 2FA answers.
func (m *Model) authorityAnswersCmd(answers []string) tea.Cmd {
	fn := m.auth.ReconnectWithAnswers
	if fn == nil {
		m.resetAuthorityRetry()
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return authorityReconnectedMsg{err: fn(ctx, answers)}
	}
}

// reportAuthorityReconnect puts the outcome of a reconnect on the status row. The
// segment itself is read while the row is built, so no further state is kept here.
func (m *Model) reportAuthorityReconnect(err error) {
	if err != nil {
		m.quietNotice(uiTextVars("gvim.notice.ssh_reconnect_failed", "SSH reconnect failed: %{error}", map[string]any{"error": err.Error()}))
		return
	}
	m.quietNotice(uiText("gvim.notice.ssh_reconnected_bare", "SSH reconnected"))
}

// SetShellRunner routes shell_command and format_buffer through fn instead of
// editor.RunShellJob. The app gives the workspace authority's one-shot as fn, so the
// command runs on the backend the workspace lives on. Nil keeps the local runner.
func (m *Model) SetShellRunner(fn func(editor.ShellJob) (string, error)) { m.shellRun = fn }
