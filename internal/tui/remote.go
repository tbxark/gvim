package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/popup"
	"github.com/tbxark/gvim/internal/tui/textsafe"
)

// remoteChoice is the data of an actionable Remote Indicator row.
type remoteChoice struct{ action string }

// showRemoteMenu opens the Remote Indicator menu (Fresh's global F6). The menu
// reports the actual workspace authority: a local session, or the SSH authority
// label/user@host with its connected/reconnecting/disconnected state and the remote
// workspace root. A remote session also offers the existing reconnect action. The
// menu never performs IO: reconnect runs in the command it returns.
func (m *Model) showRemoteMenu() {
	items := []popup.Item{}
	st, remote := m.remoteStatus()
	if remote {
		label := remoteLine(st.Text)
		authority := strings.TrimPrefix(label, "SSH:")
		if authority == "" {
			authority = label
		}
		if authority == "" {
			authority = remoteLine(m.fileRoot)
		}
		items = append(items, popup.Item{Label: uiText("gvim.remote.workspace_remote", "Workspace: Remote"), Detail: authority})
		switch {
		case st.Connecting:
			items = append(items, popup.Item{Label: uiText("gvim.remote.connecting", "Connecting…")})
		case st.Connected:
			items = append(items, popup.Item{Label: uiText("gvim.remote.connected", "Connected")})
		default:
			detail := remoteLine(st.Reason)
			if detail == "" {
				detail = uiText("gvim.remote.disconnected_detail", "activate the window to reconnect")
			}
			items = append(items, popup.Item{Label: uiText("gvim.remote.disconnected", "Disconnected"), Detail: detail})
		}
		if root := remoteLine(m.fileRoot); root != "" {
			items = append(items, popup.Item{Label: uiText("gvim.remote.remote_root", "Remote root"), Detail: root})
		}
		if m.auth.Status != nil && m.auth.Reconnect != nil {
			items = append(items, popup.Item{
				Label:  uiText("gvim.remote.reconnect", "Reconnect SSH"),
				Detail: uiText("gvim.remote.reconnect_detail", "reconnect the workspace authority"),
				Data:   remoteChoice{action: "reconnect"},
			})
		}
	} else {
		items = append(items,
			popup.Item{Label: uiText("gvim.remote.workspace_local", "Workspace: Local"), Detail: uiText("gvim.remote.on_this_machine", "on this machine")},
			popup.Item{Label: uiText("gvim.remote.no_remote_authority", "No remote authority in this session"), Detail: uiText("gvim.remote.local_hint", "start gvim with an SSH workspace to connect")},
		)
	}
	m.ShowPopup(popup.Point{X: 0, Y: m.height - 1}, uiText("gvim.remote.title", "Remote"), items)
}

// remoteLine makes one untrusted authority field paintable in one popup row: textsafe
// removes invalid UTF-8, Cc and Cf (terminal escapes, bidi overrides, zero-width
// format), then a tab or newline becomes a space so a malicious label cannot break the
// popup's row. No sync IO.
func remoteLine(s string) string {
	s = textsafe.Clean(s)
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		return r
	}, s)
}

// remoteStatus reads the current workspace authority without IO. A remote workspace
// is an SSH authority label or a remote file root; both are set by the app before the
// model is built.
func (m *Model) remoteStatus() (AuthorityStatus, bool) {
	var st AuthorityStatus
	if m.auth.Status != nil {
		st = m.auth.Status()
	}
	return st, st.Text != "" || m.fileRoot != ""
}

// remoteChosen runs a Remote Indicator action row. It reports whether the popup row
// was this menu's.
func (m *Model) remoteChosen(c popup.Chosen) (tea.Cmd, bool) {
	choice, ok := c.Item.Data.(remoteChoice)
	if !ok {
		return nil, false
	}
	switch choice.action {
	case "reconnect":
		if m.auth.Status == nil || m.auth.Reconnect == nil {
			m.notice = uiText("gvim.remote.no_authority", "No remote authority to reconnect")
			return nil, true
		}
		st := m.auth.Status()
		switch {
		case st.Connected:
			m.notice = uiText("gvim.remote.already_connected", "Already connected")
			return nil, true
		case st.Connecting || m.authorityBusy:
			m.notice = uiText("gvim.remote.reconnect_busy", "Reconnect already in progress")
			return nil, true
		}
		if cmd := m.beginAuthorityReconnect(); cmd != nil {
			return cmd, true
		}
		if m.authorityBusy {
			// A password/passphrase/2FA prompt opened; its overlay owns the next steps.
			return nil, true
		}
		m.notice = uiText("gvim.remote.reconnect_busy", "Reconnect already in progress")
	}
	return nil, true
}
