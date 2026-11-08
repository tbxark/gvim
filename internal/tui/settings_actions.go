package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/pluginapi"
)

// settingsUIActionMsg asks the root to run a TUI action once the Settings
// overlays have closed. A structure page cannot push an overlay from its own
// Update (the overlay stack updates its top slot after the call), so the actions
// it starts — the Keybinding Editor — travel this way.
type settingsUIActionMsg struct{ action string }

// settingsActDoneMsg is the result of a structure row's live action
// (SettingsPort.Act): an LSP server started/stopped, a plugin reloaded. action and
// value name the row's action, so the root can run the part that must happen on
// the update goroutine (the plugin runtime's Control is documented
// single-goroutine and is called from Update, not from the command).
type settingsActDoneMsg struct {
	page    *settingsStructurePage
	action  string
	value   string
	message string
	err     error
}

// settingsPluginControl is the update-goroutine half of a Plugins page action:
// after Act wrote and reloaded the config, Control applies the change to the live
// runtime (a plugin's enable/disable or a reload of every plugin).
func (m *Model) settingsPluginControl(action, value string) tea.Cmd {
	var op string
	switch action {
	case "plugin-enable":
		op = "enable"
	case "plugin-disable":
		op = "disable"
	case "plugin-reload":
		op = "reload_all"
	}
	if op == "" {
		return nil
	}
	return m.pluginControl(pluginapi.Control{Op: op, Name: value})
}

// settingsKeybindingRows appends the plugin bindings the keymap layers over its
// preset to the page's read-only rows, before the action row: the plugin binding
// store lives in the root model, so the app-side structure cannot list them. They
// are display-only (a plugin owns its bindings).
func (m *Model) settingsKeybindingRows(rows []SettingsStructureRow) []SettingsStructureRow {
	if m.plug.rt == nil {
		return rows
	}
	extra := make([]SettingsStructureRow, 0, 8)
	for _, b := range m.plugBindingsInOrder() {
		ctx := "normal"
		if len(b.When) > 0 {
			ctx = b.When[0]
		}
		extra = append(extra, SettingsStructureRow{
			Name: keymap.FormatChord(b.Chord), Value: b.Action, Group: ctx,
			Note: "plugin: " + b.Plugin, Locked: true,
		})
	}
	if len(extra) == 0 {
		return rows
	}
	// The action row the app put last stays last; plugin groups sit before it.
	insert := len(rows)
	for insert > 0 && rows[insert-1].Action != "" {
		insert--
	}
	out := make([]SettingsStructureRow, 0, len(rows)+len(extra))
	out = append(out, rows[:insert]...)
	out = append(out, extra...)
	out = append(out, rows[insert:]...)
	return out
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
