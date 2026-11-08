package app

import (
	"strings"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/keymap"
)

// The Settings Keybindings page. gvim already has the validated Keybinding Editor
// overlay (commands: Open Keybinding Editor): it captures keys, previews the
// changed keymap and writes the user's keybindings through the loader. This page
// is the read-only effective list beside it — preset rows with the user's own
// overrides marked, grouped by the contexts the editor resolves — plus the action
// row that opens that editor. That split is deliberate: no in-page capture and no
// second writer of the keybindings file (docs/settings.md).

// structureKeybindings is the effective binding list of cfg, grouped by the
// consulted context order, with the user's own entries marked and an action row
// that opens the Keybinding Editor.
func (s *settingsService) structureKeybindings(cfg config.Config) tui.SettingsStructure {
	st := tui.SettingsStructure{
		Key: "keybindings", Title: settingsPage("keybindings", "Keybindings"), Kind: "bindings", Filter: true,
	}
	st.Note = "Read-only: edit in the Keybinding Editor (Enter on the row below, or Ctrl+P › Open Keybinding Editor). Plugin bindings are read-only."
	if s.rt.presetName != "" {
		st.Note = "Keymap: " + s.rt.presetName + ". " + st.Note
	}
	user := userChords(cfg)
	byContext := map[string][]tui.SettingsStructureRow{}
	if s.rt.keymapOf != nil {
		if lister, ok := s.rt.keymapOf(cfg).(tui.BindingLister); ok {
			for _, b := range lister.Bindings() {
				// A preset binding without a when-clause runs in the normal context.
				ctx := b.Context
				if ctx == "" {
					ctx = string(keymap.ContextNormal)
				}
				note := "preset"
				if user[ctx+"\x00"+keymap.FormatChord(b.Chord)] {
					note = "user"
				}
				byContext[ctx] = append(byContext[ctx], tui.SettingsStructureRow{
					Name: keymap.FormatChord(b.Chord), Value: b.Action, Group: ctx,
					Note: note, Locked: true,
				})
			}
		}
	}
	for _, ctx := range keymap.ConsultedContexts {
		st.Rows = append(st.Rows, byContext[string(ctx)]...)
	}
	// The row that opens the editor. The TUI inserts the plugin rows before it.
	st.Rows = append(st.Rows, tui.SettingsStructureRow{
		Name: "Open Keybinding Editor…", Action: "ui:open_keybinding_editor",
		Note: "add, change or remove bindings; the loader validates them",
	})
	return st
}

// userChords is the set of "context\x00chord" the user config binds, for the row
// notes. It is display-only: the loader validates the file itself.
func userChords(cfg config.Config) map[string]bool {
	out := map[string]bool{}
	if len(cfg.Keybindings) == 0 {
		return out
	}
	own, _ := keymap.ParseUserBindings(cfg.Keybindings, func(string) bool { return true })
	for _, b := range own {
		chord := keymap.FormatChord(b.Chord)
		if len(b.Sequence) > 0 {
			steps := make([]string, len(b.Sequence))
			for i, step := range b.Sequence {
				steps[i] = keymap.FormatChord(step)
			}
			chord = strings.Join(steps, " ")
		}
		ctx := string(b.When)
		if ctx == "" {
			ctx = string(keymap.ContextNormal)
		}
		out[ctx+"\x00"+chord] = true
	}
	return out
}
