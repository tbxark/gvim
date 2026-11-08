package tui

import (
	"slices"

	commands "github.com/tbxark/gvim/commands"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/keymap"
)

// paletteActions are the actions that open the palette. A keymap binds the shortcut to
// either (the emacs preset binds M-x to quick_open, so that the status bar names it).
var paletteActions = []string{"quick_open", "command_palette"}

// primaryChord returns the canonical chord that the active keymap binds to the first of
// actions that has one in the normal or global context, as the palette picks it. The
// chord is empty when none has. asked is false when the keymap cannot list its bindings,
// and the callers then keep their own defaults.
func (m *Model) primaryChord(actions ...string) (chord string, asked bool) {
	l, ok := m.keys.(BindingLister)
	if !ok {
		return "", false
	}
	for _, a := range actions {
		if b, ok := l.Primary(a); ok && (b.Context == "normal" || b.Context == "global") {
			return b.Chord, true
		}
	}
	return "", true
}

// KeyLabel implements menubar.KeyLabeler: the shortcut of a menu action in the active
// keymap, formatted as the palette shows it.
func (m *Model) KeyLabel(action string) (string, bool) {
	actions := []string{action}
	if action == "command_palette" {
		actions = []string{"command_palette", "quick_open"}
	}
	chord, asked := m.primaryChord(actions...)
	if chord == "" {
		return "", asked
	}
	return commands.FormatChord(chord), true
}

// shortcutOf is the shortcut of action in the active keymap, as the palette shows it, or
// "" when it has none or the keymap cannot be asked. Unlike the menus, it also reads the
// file explorer and terminal contexts, so the explorer's own keys can be named in the tour.
func (m *Model) shortcutOf(action string) string {
	l, ok := m.keys.(BindingLister)
	if !ok {
		return ""
	}
	b, ok := l.Primary(action)
	if !ok || !keymap.Consulted(keymap.Context(b.Context)) {
		return ""
	}
	return commands.FormatChord(b.Chord)
}

// relabelCommands gives the palette's built-in commands the shortcuts of the active
// keymap. The root calls it after the keymap changed.
func (m *Model) relabelCommands() {
	l, ok := m.keys.(BindingLister)
	if !ok {
		return
	}
	cmds := slices.Clone(m.commands)
	for i := range cmds {
		cmds[i].Keybinding = ""
		if b, ok := l.Primary(cmds[i].Action); ok && (b.Context == "normal" || b.Context == "global") {
			cmds[i].Keybinding = commands.FormatChord(b.Chord)
		}
	}
	m.commands = cmds
}

// paletteHint is the text of the status row's palette hint: the shortcut of the
// palette in the active keymap. It is empty when the keymap cannot be asked.
func (m *Model) paletteHint() string {
	chord, asked := m.primaryChord(paletteActions...)
	switch {
	case !asked:
		return ""
	case chord == "":
		return "Palette"
	}
	return "Palette: " + commands.FormatChord(chord)
}

// welcomeDoorKeys are the shortcuts of the welcome doors in the active keymap, as
// the welcome screen writes them, or nil when the welcome screen is not shown or the
// keymap cannot be asked. A door whose action has no shortcut has an empty one.
func (m *Model) welcomeDoorKeys() []string {
	if !m.welcomeVisible() {
		return nil
	}
	keys := make([]string, len(frame.WelcomeDoors))
	for i, d := range frame.WelcomeDoors {
		actions := []string{d.Action}
		if d.Action == "command_palette" {
			actions = paletteActions
		}
		chord, asked := m.primaryChord(actions...)
		if !asked {
			return nil
		}
		keys[i] = commands.FormatChord(chord)
	}
	return keys
}
