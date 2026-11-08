package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/theme"
)

// A when-clause name that no condition of the editor and no plugin context has is a typo: the
// notice says so, and the warnings list names it once.
func TestUnknownWhenNameIsNamed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		when     []string
		contexts []string // set by a plugin before
		unknown  string   // "" when the clause is fine
	}{
		{"a typo", []string{"has_bufferr"}, nil, "has_bufferr"},
		{"a typo that is negated", []string{"!has_bufferr"}, nil, "has_bufferr"},
		{"a known condition", []string{"has_buffer", "!modified", "language:go", "mode:vi:vi_normal"}, nil, ""},
		{"a menu condition", []string{"modified", "has_selection"}, nil, ""},
		{"a context of a plugin", []string{"my_ctx"}, []string{"my_ctx"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rt := newFakeRuntime(t, "x.cmd")
			m, _ := newPluginModel(t, rt)
			for _, c := range tt.contexts {
				m.plug.host.SetContext("x", c, false) // set, and off now
			}
			m.plug.host.SetCommands("x", []pluginapi.CommandInfo{{ID: "x.cmd", Title: "Do It", Plugin: "x", When: tt.when, Record: true}})
			_ = m.Run("x.cmd")
			_ = m.Run("x.cmd")
			var warned []string
			for _, w := range m.notes.items {
				if strings.Contains(w, "condition") {
					warned = append(warned, w)
				}
			}
			if tt.unknown == "" {
				if len(warned) != 0 {
					t.Errorf("a good clause was reported: %q", warned)
				}
				return
			}
			if len(warned) != 1 || !strings.Contains(warned[0], `'`+tt.unknown+`'`) {
				t.Errorf("warnings = %q, want one that names %q", warned, tt.unknown)
			}
		})
	}
}

// unknownActionEditor is an editor that has no action of any name.
type unknownActionEditor struct{ *fakeEditor }

func (e *unknownActionEditor) Do(a editor.Action, _ map[string]any) editor.Result {
	return editor.Result{Err: fmt.Errorf("%w: %s", editor.ErrUnknownAction, a)}
}

// An editor keymap entry that names an action that does not exist is skipped and reported when the
// config loads (keybindingWarnings), so pressing it says nothing more: the track warns on presses
// only for keys a plugin bound (plugin_warn_test.go).
func TestKeyOfAnUnknownActionSaysSo(t *testing.T) {
	t.Parallel()
	ed := &unknownActionEditor{fakeEditor: newFakeEditor("hello")}
	m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.dispatch(Command{Action: "nonexistent.action"})
	if slices.ContainsFunc(m.notes.items, func(s string) bool { return strings.Contains(s, "nonexistent.action") }) {
		t.Errorf("the press warned again: %q", m.notes.items)
	}
}
