package tui

// The release check behind the status row's {update} element. The app installs
// one hook; the root runs it in a command at startup, so the network request
// never runs on the update goroutine, and paints "Update: vX.Y.Z" when a newer
// release is found. A failure is silent: the element simply stays absent.

import (
	"context"

	tea "charm.land/bubbletea/v2"
)

// UpdateInfo is a newer release the check found.
type UpdateInfo struct {
	// Version is the release version without the "v" prefix.
	Version string
	// URL is the release page, or "" when the feed named none.
	URL string
}

// UpdateHooks is the app's release check. Nil (the zero value) disables it.
type UpdateHooks struct {
	// Check runs off the update goroutine and returns the newer release, or nil
	// when this build is current, the check is not due yet, or it failed.
	Check func(ctx context.Context) *UpdateInfo
}

// SetUpdateHooks gives the root the release check. The app calls it once, after
// New, and only when the check is enabled.
func (m *Model) SetUpdateHooks(h UpdateHooks) { m.updates = h }

// updateCheckedMsg is the outcome of the startup check.
type updateCheckedMsg struct{ info *UpdateInfo }

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

// updateText is the {update} element's text: Fresh's "Update: vX.Y.Z", or "" when
// no newer release is known.
func (m *Model) updateText() string {
	if m.updateInfo == nil || m.updateInfo.Version == "" {
		return ""
	}
	return uiTextVars("gvim.status.update_available", "Update: v%{version}", map[string]any{"version": m.updateInfo.Version})
}
