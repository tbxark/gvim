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

// remoteMenuKey resolves k through the keymap context that owns the keyboard now and
// reports the show_remote_indicator_menu command. The default is Fresh's global F6
// binding, but the action is a normal keymap action: a user override can move it to
// another chord or context, and the check follows the same context precedence as
// handleKey. It deliberately runs before plugin panels, the terminal and every focus
// path so those surfaces cannot swallow the root action.
func (m *Model) remoteMenuKey(k tea.Key) (Command, bool) {
	if m.keys == nil {
		return Command{}, false
	}
	kp, _ := translateKey(k)
	contexts := m.keyContexts()
	exact, exactOK := m.keys.(ExactKeymap)
	unbind, unbindOK := m.keys.(ExactUnbindKeymap)
	for i, ctx := range contexts {
		if unbindOK && unbind.UnboundExact(kp, ctx) {
			// An unbind is a tombstone only for the remote action: consume the chord
			// when a weaker context/global would otherwise run show_remote_indicator_menu.
			// Every other unbound chord stays free for the editor/terminal/plugin path, so
			// a printable key still types.
			if remoteActionFallback(m.keys, kp, contexts[i+1:]) {
				return Command{}, true
			}
			return Command{}, false
		}
		var (
			cmd Command
			ok  bool
		)
		if exactOK {
			cmd, ok = exact.ResolveExact(kp, ctx)
		} else {
			// Legacy keymap without exact resolution: Resolve already applies the global
			// fallback, which this path cannot separate. Real adapters implement
			// ExactKeymap.
			cmd, ok = m.keys.Resolve(kp, ctx)
		}
		if !ok {
			continue
		}
		if cmd.Action == "show_remote_indicator_menu" {
			return cmd, true
		}
		// A more specific exact binding wins and is left to its own surface.
		return Command{}, false
	}
	return Command{}, false
}

// remoteActionFallback reports whether one of the weaker exact contexts would run the
// remote-menu action for kp. An exact unbind in a weaker context stops the fallback
// there (nothing would run), so the walk stops at it.
func remoteActionFallback(km Keymap, kp KeyPress, contexts []KeyContext) bool {
	if km == nil || len(contexts) == 0 {
		return false
	}
	exact, exactOK := km.(ExactKeymap)
	unbind, unbindOK := km.(ExactUnbindKeymap)
	for _, ctx := range contexts {
		if unbindOK && unbind.UnboundExact(kp, ctx) {
			return false
		}
		var (
			cmd Command
			ok  bool
		)
		if exactOK {
			cmd, ok = exact.ResolveExact(kp, ctx)
		} else {
			cmd, ok = km.Resolve(kp, ctx)
		}
		if ok {
			return cmd.Action == "show_remote_indicator_menu"
		}
	}
	return false
}

// keyContexts returns the keymap context(s) of the current focus. The editor and the
// native sections use normal; the explorer has its own context; the tab strip, a
// focused plugin panel and a focused terminal dock have no section context of their own
// (dock/terminal) and fall back to global. The list order is precedence.
func (m *Model) keyContexts() []KeyContext {
	switch {
	case m.dockKeyboard():
		return []KeyContext{KeyContextDock, KeyContextTerminal, KeyContextGlobal}
	case m.explorerFocus:
		return []KeyContext{KeyContextFileExplorer, KeyContextGlobal}
	case m.tabFocus:
		return []KeyContext{KeyContextGlobal}
	case m.plug.panels.focus != 0:
		return []KeyContext{KeyContextGlobal}
	default:
		return []KeyContext{KeyContextNormal, KeyContextGlobal}
	}
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
