package keymap_test

import (
	"slices"
	"testing"

	"github.com/tbxark/gvim/internal/keymap"
)

// commonActions is the set of actions every preset must keep a default key for: the common
// shortcuts, file, edit, search and navigation, tabs and splits, view, multi-cursor and the
// language-server keys. Everything else is reached through the command palette and the menus,
// with no default key. Each preset spells the keys in its own idiom, and binds each of these
// actions to one chord. "toggle_utility_dock" is the terminal toggle; "focus_split" is
// next_split, the one split-focus key the presets keep.
var commonActions = []string{
	// file
	"new", "open", "save", "save_as", "quit",
	// edit
	"undo", "redo", "cut", "copy", "paste", "select_all", "duplicate_line", "delete_line",
	"move_line_up", "move_line_down", "toggle_comment", "dedent_selection", "insert_tab",
	// search and navigation
	"search", "find_next", "find_previous", "goto_line", "quick_open", "open_live_grep",
	"navigate_back", "navigate_forward", "goto_matching_bracket",
	"move_word_left", "move_document_start", "move_document_end", "move_page_up", "move_page_down",
	"select_word_left", "select_document_start",
	// tabs and splits
	"next_buffer", "prev_buffer", "split_vertical", "split_horizontal", "close_split", "next_split",
	// view
	"toggle_file_explorer", "toggle_fold", "toggle_utility_dock",
	// multi-cursor
	"add_cursor_next_match", "add_cursor_above", "add_cursor_below", "remove_secondary_cursors",
	// LSP
	"lsp_goto_definition", "lsp_rename", "lsp_hover", "lsp_references",
}

// replaceActions are the actions of which a preset needs one: the presets spell Replace
// differently (emacs has only query_replace).
var replaceActions = []string{"replace", "query_replace"}

// closeActions: Emacs closes with C-x k (close), the others with close_tab.
var closeActions = []string{"close_tab", "close"}

// paletteActions: the preset needs one key that opens the palette.
var paletteActions = []string{"command_palette", "quick_open"}

func TestEveryPresetKeepsTheCommonSet(t *testing.T) {
	for _, name := range []string{"default", "macos", "macos-gui", "emacs", "vscode"} {
		m := mustPreset(t, name)
		for _, a := range commonActions {
			if _, ok := m.Primary(a); !ok {
				t.Errorf("%s: common action %s has no shortcut", name, a)
			}
		}
		for _, group := range [][]string{replaceActions, paletteActions, closeActions} {
			if !slices.ContainsFunc(group, func(a string) bool { _, ok := m.Primary(a); return ok }) {
				t.Errorf("%s: none of %v has a shortcut", name, group)
			}
		}
	}
}

// TestPresetsStayCommon pins the size of the pruning. A preset has about 60 to 80 bindings that run
// an editor action in the normal or global context: a chord that resolves to the action it
// names, and whose key does not start a multi-key sequence (as the single C-x of emacs does).
// Noop and unbind entries are not counted. Fresh's default file has about 270 bindings. A new
// default key must be argued for, so the ceiling is tight. Each preset binds each common action
// once, so the count is close to the number of common actions.
func TestPresetsStayCommon(t *testing.T) {
	for _, name := range []string{"default", "macos", "macos-gui", "emacs", "vscode"} {
		m := mustPreset(t, name)
		seen := map[string]bool{}
		for _, b := range m.Bindings() {
			if (b.When != keymap.ContextNormal && b.When != keymap.ContextGlobal) || b.Action == keymap.ActionNoop || b.Action == keymap.ActionUnbind {
				continue
			}
			if b.Sequence == nil && m.HasSequencePrefix([]string{b.Chord}, b.When) {
				continue
			}
			var got keymap.Binding
			var ok bool
			if b.Sequence != nil {
				got, ok = m.ResolveSequence(b.Sequence, keymap.ContextNormal)
			} else {
				got, ok = m.Resolve(b.Chord, keymap.ContextNormal)
			}
			if ok && got.Action == b.Action {
				seen[b.Chord+" "+b.Action] = true
			}
		}
		if n := len(seen); n < 60 || n > 80 {
			t.Errorf("%s has %d editor bindings, want 60 to 80", name, n)
		}
	}
}

// menuActions are the actions of the removed menu-activation keys (F10, Alt+letter).
func TestNoPresetBindsMenuActivation(t *testing.T) {
	for _, name := range []string{"default", "macos", "macos-gui", "emacs", "vscode"} {
		m := mustPreset(t, name)
		for _, b := range m.Bindings() {
			if b.Action == "menu_activate" || b.Action == "menu_open" {
				t.Errorf("%s binds %s to %s in %s", name, b.Chord, b.Action, b.When)
			}
		}
		for _, chord := range []string{"F10", "alt+f", "alt+e", "alt+v", "alt+s", "alt+g", "alt+h", "alt+x"} {
			if b, ok := m.Resolve(chord, keymap.ContextNormal); ok && (b.Action == "menu_open" || b.Action == "menu_activate") {
				t.Errorf("%s: %s opens a menu", name, chord)
			}
		}
	}
}

// TestRemovedKeysAreFree pins a few of the keys that left the default set, so that they do
// not creep back: the bookmark, macro, tour and compare keys of Fresh, Alt+A (it labelled a
// project-wide replace that only searched the buffer) and the quick-open shortcuts.
func TestRemovedKeysAreFree(t *testing.T) {
	m := mustPreset(t, "default")
	for _, chord := range []string{"alt+a", "F4", "F5", "alt+0", "ctrl+shift+1", "ctrl+alt+n", "alt+F5", "ctrl+'", "ctrl+;", "alt+u", "alt+l"} {
		if b, ok := m.Resolve(chord, keymap.ContextNormal); ok {
			t.Errorf("default still binds %s to %s", chord, b.Action)
		}
	}
}

// TestPresetIdioms checks that each preset keeps its own key style.
func TestPresetIdioms(t *testing.T) {
	tests := []struct {
		preset string
		steps  []string
		action string
	}{
		{"emacs", []string{"ctrl+x", "ctrl+s"}, "save"},
		{"emacs", []string{"ctrl+x", "ctrl+f"}, "open"},
		{"emacs", []string{"ctrl+x", "ctrl+c"}, "quit"},
		{"emacs", []string{"ctrl+x", "2"}, "split_horizontal"},
		{"emacs", []string{"ctrl+x", "o"}, "next_split"},
		{"emacs", []string{"alt+g", "g"}, "goto_line"},
		{"vscode", []string{"ctrl+shift+f"}, "open_live_grep"},
		{"vscode", []string{"ctrl+k", "ctrl+l"}, "toggle_fold"},
		{"vscode", []string{"ctrl+w"}, "close_tab"},
		{"macos", []string{"ctrl+t"}, "command_palette"},
		{"macos", []string{"alt+s"}, "save_as"},
		{"macos-gui", []string{"super+s"}, "save"},
		{"macos-gui", []string{"super+shift+p"}, "command_palette"},
		{"macos-gui", []string{"super+d"}, "add_cursor_next_match"},
	}
	for _, tc := range tests {
		b, ok := mustPreset(t, tc.preset).ResolveSequence(tc.steps, keymap.ContextNormal)
		if !ok {
			b, ok = mustPreset(t, tc.preset).ResolveSequence(tc.steps, keymap.ContextGlobal)
		}
		if !ok || b.Action != tc.action {
			t.Errorf("%s %v = %q (%v), want %s", tc.preset, tc.steps, b.Action, ok, tc.action)
		}
	}
}

// TestErrorJumpKeys pins the keys of Jump to Next Error and Jump to Previous Error. F8 and
// Shift+F8 are Fresh's keys in its default keymap (Navigation in docs/features/editing.md), and
// the default and VS Code presets inherit them. The macOS preset moves the next-error key to
// Ctrl+' as Fresh's macos.json does, because F8 is a media key there, and leaves previous error to
// the palette. Emacs and macOS set no F8 key, which keeps both presets within the binding ceiling
// of TestPresetsStayCommon.
func TestErrorJumpKeys(t *testing.T) {
	tests := []struct {
		preset, chord, action string
	}{
		{"default", "F8", "jump_to_next_error"},
		{"default", "shift+F8", "jump_to_previous_error"},
		{"vscode", "F8", "jump_to_next_error"},
		{"vscode", "shift+F8", "jump_to_previous_error"},
		{"macos", "ctrl+'", "jump_to_next_error"},
		{"macos", "F8", ""},
		{"macos", "shift+F8", ""},
		{"emacs", "F8", ""},
		{"emacs", "shift+F8", ""},
	}
	for _, tt := range tests {
		m := mustPreset(t, tt.preset)
		b, ok := m.Resolve(tt.chord, keymap.ContextNormal)
		if tt.action == "" {
			if ok && (b.Action == "jump_to_next_error" || b.Action == "jump_to_previous_error") {
				t.Errorf("%s: %s still jumps to an error (%s)", tt.preset, tt.chord, b.Action)
			}
			continue
		}
		if !ok || b.Action != tt.action {
			t.Errorf("%s: %s resolves to %q (%t), want %q", tt.preset, tt.chord, b.Action, ok, tt.action)
		}
	}
}
