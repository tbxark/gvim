package tui

// The release check behind the status row's {update} element, and the two
// commands around it. The app installs the hooks; the root runs the check in a
// command, so the network request never runs on the update goroutine, and paints
// "Update: vX.Y.Z" when a newer release is found. A failed startup check is
// silent: the element simply stays absent. The commands (Check for Updates and
// Show Update Output) are the explicit way to ask and to read the details.

import (
	"context"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/list"
)

// UpdateInfo is a newer release the check found.
type UpdateInfo struct {
	// Version is the release version without the "v" prefix.
	Version string
	// URL is the release page, or "" when the feed named none.
	URL string
}

// UpdateStatus is what the app knows about the release check: the last answer,
// when it was obtained, and why a check failed. The zero value means the check
// has not run (or is disabled).
type UpdateStatus struct {
	// Current is this build's version, or "" for a development build.
	Current string
	// Latest is the newest release the last check found, or "" when the build was
	// current (or no check has succeeded).
	Latest string
	// Available reports whether Latest is newer than Current.
	Available bool
	// URL is the release page from the feed, or "".
	URL string
	// Feed is the release feed URL that was polled.
	Feed string
	// CheckedAt is when the last successful check ran; zero means never.
	CheckedAt time.Time
	// Err explains why the last check failed, or "" when it succeeded.
	Err string
}

// UpdateHooks is the app's release check. Nil (the zero value) disables it, and
// the commands then say so instead of doing nothing.
type UpdateHooks struct {
	// Command is the configured update command argv (update_command, through the
	// user's shell). Empty hides the Update fresh/update-output rows and leaves the
	// actions to their not-available notice.
	Command []string
	// Check runs off the update goroutine and returns the newer release, or nil
	// when this build is current, the check is not due yet, or it failed.
	Check func(ctx context.Context) *UpdateInfo
	// Force runs a check now, ignoring the daily debounce, and returns the state
	// it leaves behind (including a failure reason).
	Force func(ctx context.Context) UpdateStatus
	// Status returns the state the app keeps: what the last check found or why it
	// failed. Nil means the app keeps no state (a test model).
	Status func() UpdateStatus
}

// SetUpdateHooks gives the root the release check. The app calls it once, after
// New, and only when the check is enabled.
func (m *Model) SetUpdateHooks(h UpdateHooks) { m.updates = h }

// updateCheckedMsg is the outcome of the startup check.
type updateCheckedMsg struct{ info *UpdateInfo }

// updateForcedMsg is the outcome of Check for Updates.
type updateForcedMsg struct{ status UpdateStatus }

// updateCheckCmd runs the release check once at startup, or returns nil when no
// hook is installed.
func (m *Model) updateCheckCmd() tea.Cmd {
	check := m.updates.Check
	if check == nil {
		return nil
	}
	return func() tea.Msg {
		return updateCheckedMsg{info: check(context.Background())}
	}
}

// updateForceCmd runs the check now, ignoring the debounce, as a command (the
// request never runs on the update goroutine).
func (m *Model) updateForceCmd() tea.Cmd {
	force := m.updates.Force
	if force == nil {
		return nil
	}
	return func() tea.Msg {
		return updateForcedMsg{status: force(context.Background())}
	}
}

// updateText is the {update} element's text: Fresh's "Update: vX.Y.Z", or "" when
// no newer release is known.
func (m *Model) updateText() string {
	if m.updateInfo == nil || m.updateInfo.Version == "" {
		return ""
	}
	return uiTextVars("gvim.status.update_available", "Update: v%{version}", map[string]any{"version": m.updateInfo.Version})
}

// updateAction runs the two update commands, and reports whether it handled the
// action.
func (m *Model) updateAction(action string) (tea.Cmd, bool) {
	switch action {
	case "check_for_updates":
		if m.updates.Force == nil {
			m.notice = "Update checking is off (check_for_updates is false)."
			return nil, true
		}
		m.notice = "Checking for updates..."
		return m.updateForceCmd(), true
	case "show_update_output":
		m.openUpdateOutput()
		return nil, true
	case "update_fresh":
		if len(m.updates.Command) == 0 {
			return nil, false // notApplicableNotice keeps the old "does not update itself"
		}
		return m.runUpdateCommand(), true
	case "open_update_log":
		if len(m.updates.Command) == 0 {
			return nil, false
		}
		return m.focusUpdateOutput(), true
	}
	return nil, false
}

// applyUpdateStatus stores the state a forced check left behind and reports it in
// the status row. A found release also paints the {update} element, so the two
// commands and the startup check agree.
func (m *Model) applyUpdateStatus(st UpdateStatus) {
	if st.Err != "" {
		m.notice = "Update check failed: " + st.Err
		return
	}
	if st.Available {
		info := &UpdateInfo{Version: st.Latest, URL: st.URL}
		m.updateInfo = info
		msg := "gvim " + st.Latest + " is available"
		if st.URL != "" {
			msg += ": " + st.URL
		}
		if st.Current != "" {
			msg += " (this build is " + st.Current + ")"
		}
		m.notice = msg
		return
	}
	m.updateInfo = nil
	current := st.Current
	if current == "" {
		current = "this build"
	}
	m.notice = "gvim is up to date (" + current + ")."
}

// openUpdateOutput lists what the release check knows: the feed, the versions,
// when the last check ran and the release page of the newest release. gvim does
// not install releases, so this is Fresh's "Show update output" without an
// updater log (docs/ui.md).
func (m *Model) openUpdateOutput() {
	var st UpdateStatus
	if m.updates.Status != nil {
		st = m.updates.Status()
	}
	rows := []list.Row{
		{Left: "Feed", Right: orDash(st.Feed)},
		{Left: "Current version", Right: orDash(st.Current)},
		{Left: "Latest release", Right: orDash(st.Latest)},
		{Left: "Status", Right: updateStateLabel(st)},
		{Left: "Last checked", Right: updateCheckedLabel(st.CheckedAt)},
		{Left: "Release page", Right: orDash(st.URL)},
	}
	v := list.New("Update", rows, m.listChrome(), func(v *list.View, r list.Row) tea.Cmd {
		v.Close()
		return nil
	})
	v.Fit = true
	m.pushOverlay(v)
}

// orDash keeps an empty value readable in the update list.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// updateStateLabel is the Status row of the update list.
func updateStateLabel(st UpdateStatus) string {
	switch {
	case st.Err != "":
		return "check failed: " + st.Err
	case st.Available:
		return "update available"
	case st.CheckedAt.IsZero():
		return "not checked yet"
	default:
		return "up to date"
	}
}

// updateCheckedLabel is the Last checked row: Fresh's idea of a readable time,
// without a locale-dependent formatter.
func updateCheckedLabel(at time.Time) string {
	if at.IsZero() {
		return "never"
	}
	h, mnt := at.Hour(), at.Minute()
	return at.Format("2006-01-02") + " " + two(h) + ":" + two(mnt)
}

// two zero-pads a clock field.
func two(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
