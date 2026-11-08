package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/keymap"
)

// chordKey takes the key press kp when it belongs to a multi-key binding, such as
// Emacs's C-x C-s. It reports whether it took the press. A press that begins such a
// binding is held, and wins over the single-key binding of the same key: the first key
// of a sequence never runs its own command. The press that completes the binding runs
// it. Any other press cancels the sequence and is dropped, as Emacs does, so a mistyped
// C-x never edits the text.
func (m *Model) chordKey(kp KeyPress, ctx KeyContext) (tea.Cmd, bool) {
	sk, ok := m.keys.(SequenceKeymap)
	if !ok {
		m.chord = nil
		return nil, false
	}
	steps := append(append([]KeyPress(nil), m.chord...), kp)
	cmd, found, prefix := sk.ResolveSequence(steps, ctx)
	switch {
	case found:
		m.chord = nil
		return m.dispatch(cmd), true
	case prefix:
		m.chord = steps
		m.notice = chordLabel(steps) + "-"
		return nil, true
	case len(m.chord) > 0:
		m.notice = chordLabel(steps) + " is not bound"
		m.chord = nil
		return nil, true
	}
	return nil, false
}

// chordLabel spells the key presses of a sequence for the status row, in the one label
// style the menus and palette use ("Ctrl+X Ctrl+S").
func chordLabel(steps []KeyPress) string {
	parts := make([]string, len(steps))
	for i, s := range steps {
		name := s.Name
		if name == " " {
			name = "space"
		}
		parts[i] = strings.Join(append(append([]string(nil), s.Mods...), name), "+")
	}
	return keymap.FormatChord(strings.Join(parts, " "))
}
