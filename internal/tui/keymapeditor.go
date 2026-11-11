package tui

import (
	"errors"

	tea "charm.land/bubbletea/v2"
	"github.com/tbxark/gvim/internal/tui/list"
)

// keymapPresets are the built-in keymaps the switch lists, in menu order.
var keymapPresets = []string{"default", "macos", "macos-gui", "emacs", "vscode"}

// openKeymapSwitch lists the built-in keymaps. The active one is marked, and
// choosing a row switches to it.
func (m *Model) openKeymapSwitch() {
	rows := make([]list.Row, len(keymapPresets))
	for i, name := range keymapPresets {
		right := ""
		// The same rule as the menu's check mark: no keymap name yet means default.
		if m.util.keymapChecked("keymap_" + name) {
			right = "current"
		}
		rows[i] = list.Row{Left: name, Right: right, Value: name}
	}
	v := list.New("Keymap", rows, m.listChrome(), func(v *list.View, r list.Row) tea.Cmd {
		v.Close()
		m.switchKeymap(r.Value)
		return nil
	})
	v.Hint = "Type to filter, Up/Down move, Enter switch, Esc close"
	m.pushOverlay(v)
}

// switchKeymap replaces the keymap the next key press resolves in with the preset
// called name. The status row confirms the switch. A failed switch keeps the
// current keymap and says why.
func (m *Model) switchKeymap(name string) {
	if m.util.switchKeymap == nil {
		m.notice = "Keymap switching is not supported in this port"
		return
	}
	if !m.applyKeymap(name) {
		return
	}
	m.notice = "Keymap: " + name + " for this session; set \"keymap\" in the config to keep it"
}

// applyKeymap makes the preset called name the live keymap, with the plugin bindings
// layered on it, and reports whether it did. A failure says why on the status row.
func (m *Model) applyKeymap(name string) bool {
	if m.util.switchKeymap == nil {
		return false
	}
	k, err := m.util.switchKeymap(name)
	if err == nil && k == nil {
		err = errNoKeymap
	}
	if err != nil {
		m.notice = "Keymap " + name + ": " + err.Error()
		return false
	}
	m.keys = m.pluginSwitchKeymap(name, k)
	m.resetLiveGrepResume()
	m.relabelCommands()
	m.util.settings.Keymap = name
	m.plugConfigChanged()
	return true
}

// errNoKeymap is the error of a switch that returned neither a keymap nor an error.
var errNoKeymap = errors.New("no keymap")
