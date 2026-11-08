package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/theme"
)

// seqKeymap is a fakeKeymap with multi-key bindings, keyed by "name+mods name+mods".
type seqKeymap struct {
	fakeKeymap
	seqs map[string]Command
}

func stepKey(s KeyPress) string { return strings.Join(append(slices.Clone(s.Mods), s.Name), "+") }

func (k seqKeymap) ResolveSequence(steps []KeyPress, ctx KeyContext) (Command, bool, bool) {
	var keys []string
	for _, s := range steps {
		keys = append(keys, stepKey(s))
	}
	joined := strings.Join(keys, " ")
	if c, ok := k.seqs[joined]; ok && len(steps) > 1 {
		return c, true, false
	}
	for seq := range k.seqs {
		if strings.HasPrefix(seq, joined+" ") {
			return Command{}, false, true
		}
	}
	return Command{}, false, false
}

func newSeqModel(t *testing.T) (*Model, *fakeEditor) {
	t.Helper()
	ed := newFakeEditor("one", "two")
	keys := seqKeymap{
		// ctrl+x alone is Cut, as in the Emacs preset's inherited bindings.
		fakeKeymap: fakeKeymap{"normal|x|ctrl": {Action: "cut"}, "normal|z|ctrl": {Action: "redo"}},
		seqs: map[string]Command{
			"ctrl+x ctrl+s": {Action: "undo"},
			"ctrl+x k":      {Action: "select_all"},
			"alt+g g":       {Action: "goto_line_start"},
		},
	}
	m := New(Deps{Editor: ed, Keys: keys, Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 10})
	return m, ed
}

func TestMultiKeyBindings(t *testing.T) {
	alt := func(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModAlt} }
	tests := []struct {
		name   string
		keys   []tea.KeyPressMsg
		want   []editor.Action
		notice string
	}{
		{"the first key does not run its own binding", []tea.KeyPressMsg{ctrl('x')}, nil, "Ctrl+X-"},
		{"two chords", []tea.KeyPressMsg{ctrl('x'), ctrl('s')}, []editor.Action{"undo"}, ""},
		{"chord then plain key", []tea.KeyPressMsg{ctrl('x'), typed("k")}, []editor.Action{"select_all"}, ""},
		{"alt prefix", []tea.KeyPressMsg{alt('g'), typed("g")}, []editor.Action{"goto_line_start"}, ""},
		{"a wrong second key is dropped", []tea.KeyPressMsg{ctrl('x'), typed("q")}, nil, "Ctrl+X Q is not bound"},
		{"a wrong second key cannot run its single binding", []tea.KeyPressMsg{ctrl('x'), ctrl('z')}, nil, "Ctrl+X Ctrl+Z is not bound"},
		{"escape cancels", []tea.KeyPressMsg{ctrl('x'), special(tea.KeyEscape)}, nil, "Ctrl+X Esc is not bound"},
		{"the sequence ends after a miss", []tea.KeyPressMsg{ctrl('x'), typed("q"), ctrl('z')}, []editor.Action{"redo"}, ""},
		{"the sequence can run twice", []tea.KeyPressMsg{ctrl('x'), typed("k"), ctrl('x'), typed("k")}, []editor.Action{"select_all", "select_all"}, ""},
		{"an unrelated key keeps its binding", []tea.KeyPressMsg{ctrl('z')}, []editor.Action{"redo"}, ""},
		{"typing is not held", []tea.KeyPressMsg{typed("x")}, []editor.Action{"insert_char"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, ed := newSeqModel(t)
			for _, k := range tc.keys {
				press(m, k)
			}
			if !slices.Equal(ed.actions, tc.want) {
				t.Errorf("editor actions = %v, want %v", ed.actions, tc.want)
			}
			if m.notice != tc.notice {
				t.Errorf("notice = %q, want %q", m.notice, tc.notice)
			}
		})
	}
}

func TestMultiKeyBindingDropsAtAClick(t *testing.T) {
	m, ed := newSeqModel(t)
	press(m, ctrl('x'))
	m.Update(click(5, 3, tea.MouseLeft))
	m.Update(tea.MouseReleaseMsg(tea.Mouse{X: 5, Y: 3, Button: tea.MouseLeft}))
	press(m, ctrl('s'))
	if slices.Contains(ed.actions, "undo") {
		t.Errorf("the sequence went on after a click: %v", ed.actions)
	}
}
