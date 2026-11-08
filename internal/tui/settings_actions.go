package tui

// settingsUIActionMsg asks the root to run a TUI action once the Settings
// overlays have closed. A structure page cannot push an overlay from its own
// Update (the overlay stack updates its top slot after the call), so the actions
// it starts — the Keybinding Editor — travel this way.
type settingsUIActionMsg struct{ action string }

// settingsActDoneMsg is the result of a structure row's live action
// (SettingsPort.Act): an LSP server started/stopped, a plugin reloaded.
type settingsActDoneMsg struct {
	page    *settingsStructurePage
	message string
	err     error
}

// closeSettingsOverlays closes the Settings page and every sub-page above it, so
// an action that opens another overlay starts from a clean stack.
func (m *Model) closeSettingsOverlays() {
	for len(m.overlays) > 0 {
		switch top := m.overlays[len(m.overlays)-1].(type) {
		case *settingsPage:
			top.Close()
		case *settingsStructurePage:
			top.Close()
		default:
			return
		}
		m.popOverlay()
	}
}
