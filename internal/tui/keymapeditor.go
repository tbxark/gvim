package tui

import (
	"errors"

	tea "charm.land/bubbletea/v2"
)

// keymapPresets are the built-in keymaps the switch lists, in menu order.
var keymapPresets = []string{"default", "macos", "macos-gui", "emacs", "vscode"}

// openKeymapSwitch lists the built-in keymaps. The active one is marked, and
// choosing a row switches to it.
func (m *Model) openKeymapSwitch() {
	rows := make([]listRow, len(keymapPresets))
	for i, name := range keymapPresets {
		right := ""
		if name == m.util.settings.Keymap {
			right = "current"
		}
		rows[i] = listRow{left: name, right: right, value: name}
	}
	v := newListView("Keymap", rows, m.listChrome(), func(v *listView, r listRow) tea.Cmd {
		v.active = false
		m.switchKeymap(r.value)
		return nil
	})
	v.hint = "Type to filter, Up/Down move, Enter switch, Esc close"
	m.pushOverlay(listOverlay{v: v})
}

// switchKeymap replaces the keymap the next key press resolves in with the preset
// called name. The status row confirms the switch. A failed switch keeps the
// current keymap and says why.
func (m *Model) switchKeymap(name string) {
	if m.util.switchKeymap == nil {
		m.notice = "Keymap switching is not supported in this port"
		return
	}
	k, err := m.util.switchKeymap(name)
	if err == nil && k == nil {
		err = errNoKeymap
	}
	if err != nil {
		m.notice = "Keymap " + name + ": " + err.Error()
		return
	}
	m.keys = m.pluginSwitchKeymap(name, k)
	m.relabelCommands()
	m.util.settings.Keymap = name
	m.notice = "Keymap: " + name + " for this session; set \"keymap\" in the config to keep it"
	m.plugConfigChanged()
}

// errNoKeymap is the error of a switch that returned neither a keymap nor an error.
var errNoKeymap = errors.New("no keymap")
