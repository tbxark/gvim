package tui

import (
	"github.com/tbxark/gvim/internal/tui/popup"
)

// showRemoteMenu opens the remote indicator menu. It says that the workspace is
// local and that remote workspaces are not supported. The menu is anchored at the
// bottom row, so it opens above that row.
func (m *Model) showRemoteMenu() {
	m.ShowPopup(popup.Point{X: 0, Y: m.height - 1}, "Remote", []popup.Item{
		{Label: "Workspace: Local", Detail: "on this machine"},
		{Label: "Remote workspaces are not supported"},
	})
}
