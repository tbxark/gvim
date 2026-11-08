package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/style"
)

// utilities is the state of the overlays in this group: the settings that
// open_settings lists, the keymap switch, and the input events the diagnostic
// views show. The zero value has no settings and no switch.
type utilities struct {
	settings     Settings
	settingsPort SettingsPort
	themeCatalog ThemeCatalog
	switchKeymap func(name string) (Keymap, error)
	events       eventRing
}

// utilityAction runs an action of the overlays in this group and reports whether
// the action is one of them. The root calls it before the editor, so an action
// named here never reaches the editor.
func (m *Model) utilityAction(action string) bool {
	if name, ok := strings.CutPrefix(action, "switch_keybinding_map:"); ok {
		m.switchKeymap(name)
		return true
	}
	switch action {
	case "show_help":
		m.openHelp("Help")
	case "keyboard_shortcuts":
		m.openHelp("Keyboard Shortcuts")
	case "open_keybinding_editor":
		m.openKeybindingEditor()
	case "switch_keybinding_map", "select_keybinding_map":
		m.openKeymapSwitch()
	case "open_settings":
		m.openSettings()
	case "settings_activate":
		m.openConfigFile()
	case "tour_next", "tour_prev":
		m.tourAction(action)
	case "show_remote_indicator_menu":
		m.showRemoteMenu()
	case "popup_focus":
		if !m.popupFocus() {
			m.notice = "No popup to focus"
		}
	case "calibrate_input":
		m.openCalibrate()
	case "event_debug":
		m.openEventDebug()
	default:
		return false
	}
	return true
}

// listChrome returns the overlay styles that the lists and the text boxes share.
func (m *Model) listChrome() style.Overlay {
	return m.styles.Overlay
}

// keymapChecked reports whether the menu row menuKey, such as "keymap_emacs", is
// the active keymap. Until a keymap name is known, the Default row is checked, as
// it was before the switch existed.
func (u utilities) keymapChecked(menuKey string) bool {
	name := strings.ReplaceAll(strings.TrimPrefix(menuKey, "keymap_"), "_", "-")
	active := u.settings.Keymap
	if active == "" {
		active = "default"
	}
	return active == name
}

// keyLabel splits a key press for the calibration view: the key name without its
// modifiers, the modifiers and the text, as the terminal reported them.
func keyLabel(k tea.Key) (name, mods, text string) {
	base := k
	base.Mod = 0
	name = base.Keystroke()
	var parts []string
	for _, m := range []struct {
		bit  tea.KeyMod
		name string
	}{
		{tea.ModCtrl, "ctrl"}, {tea.ModAlt, "alt"}, {tea.ModShift, "shift"},
		{tea.ModMeta, "meta"}, {tea.ModHyper, "hyper"}, {tea.ModSuper, "super"},
	} {
		if k.Mod&m.bit != 0 {
			parts = append(parts, m.name)
		}
	}
	mods = "none"
	if len(parts) > 0 {
		mods = strings.Join(parts, "+")
	}
	text = "(none)"
	if k.Text != "" {
		text = fmt.Sprintf("%q", k.Text)
	}
	return name, mods, text
}
