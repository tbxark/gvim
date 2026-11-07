package ui

import (
	"errors"

	tea "charm.land/bubbletea/v2"
)

// keymapPresets are the built-in keymaps the switch lists, in menu order.
var keymapPresets = []string{"default", "macos", "macos-gui", "emacs", "vscode"}

// openKeybindingEditor shows the bindings of the active keymap, one row each, with
// the action, the chord and the context. The list is read-only: the keybindings
// are edited in the config file.
func (m *Model) openKeybindingEditor() {
	l, _ := m.keys.(BindingLister)
	v := newListView("Keybindings", keybindingRows(l), m.listChrome(), nil)
	if l == nil {
		v.empty = noBindingsText
	}
	m.pushOverlay(listOverlay{v: v})
}

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
	m.util.settings.Keymap = name
	m.notice = "Keymap: " + name
	m.plugConfigChanged()
}

// errNoKeymap is the error of a switch that returned neither a keymap nor an error.
var errNoKeymap = errors.New("no keymap")
