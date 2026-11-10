package keymap_test

import (
	"reflect"
	"testing"

	"github.com/tbxark/gvim/keymap"
)

// presetNames are the embedded keymaps, in inheritance order for the base and its
// children.
var presetNames = []string{"default", "macos", "macos-gui", "emacs", "vscode"}

// presetCounts pins the size of every preset: the bindings in its own file, the
// bindings it holds in all (its file plus the parent's, in file order), and the bindings
// that are in effect, one per context and chord. The per-context counts pin where the
// bindings live. The presets hold Fresh's bindings for the contexts gvim resolves
// (keymap.ConsultedContexts), plus the gvim-specific keys TestGvimExtensions lists; a
// change to any count must be argued for here.
//
//	default     file 259  held 259  in effect 257  global 14 normal 146 prompt 34 searchPrompt 7 popup 6 menu 6 settings 5 explorer 30 terminal 6 dock 3
//	macos       file  60  held 319  in effect 279  global 14 normal 160 prompt 38 searchPrompt 9 popup 6 menu 6 settings 5 explorer 30 terminal 8 dock 3
//	macos-gui   file  52  held 371  in effect 330  global 16 normal 204 prompt 38 searchPrompt 11 popup 6 menu 6 settings 5 explorer 32 terminal 9 dock 3
//	emacs       file  98  held 357  in effect 322  global 14 normal 191 prompt 42 searchPrompt 9 popup 9 menu 11 settings 5 explorer 32 terminal 6 dock 3
//	vscode      file   9  held 268  in effect 262  global 14 normal 151 prompt 34 searchPrompt 7 popup 6 menu 6 settings 5 explorer 30 terminal 6 dock 3
//
// The held count is larger than the file plus the parent's file because the parent's
// overridden bindings are kept in file order (the last one wins), and "in effect" is
// smaller than held because overrides and Fresh's repeated dedent aliases leave one
// binding per context and chord.
var presetCounts = []struct {
	name         string
	held         int
	effective    int
	global       int
	normal       int
	prompt       int
	searchPrompt int
	popup        int
	menu         int
	settings     int
	explorer     int
	terminal     int
	dock         int
	file         int
}{
	{"default", 260, 258, 14, 147, 34, 7, 6, 6, 5, 30, 6, 3, 260},
	{"macos", 320, 280, 14, 161, 38, 9, 6, 6, 5, 30, 8, 3, 60},
	{"macos-gui", 372, 331, 16, 205, 38, 11, 6, 6, 5, 32, 9, 3, 52},
	{"emacs", 358, 323, 14, 192, 42, 9, 9, 11, 5, 32, 6, 3, 98},
	{"vscode", 269, 263, 14, 152, 34, 7, 6, 6, 5, 30, 6, 3, 9},
}

func TestPresetBindingCounts(t *testing.T) {
	for _, tc := range presetCounts {
		t.Run(tc.name, func(t *testing.T) {
			m := mustPreset(t, tc.name)
			if got := len(m.Bindings()); got != tc.held {
				t.Errorf("held bindings = %d, want %d", got, tc.held)
			}
			seen := map[keymap.Context]map[string]bool{}
			for _, b := range m.Bindings() {
				if seen[b.When] == nil {
					seen[b.When] = map[string]bool{}
				}
				seen[b.When][b.Chord] = true
			}
			effective := 0
			for _, chords := range seen {
				effective += len(chords)
			}
			if effective != tc.effective {
				t.Errorf("effective bindings = %d, want %d", effective, tc.effective)
			}
			contexts := []struct {
				ctx  keymap.Context
				want int
			}{
				{keymap.ContextGlobal, tc.global},
				{keymap.ContextNormal, tc.normal},
				{keymap.ContextPrompt, tc.prompt},
				{keymap.ContextSearchPrompt, tc.searchPrompt},
				{keymap.ContextPopup, tc.popup},
				{keymap.ContextMenu, tc.menu},
				{keymap.ContextSettings, tc.settings},
				{keymap.ContextFileExplorer, tc.explorer},
				{keymap.ContextTerminal, tc.terminal},
				{keymap.ContextDock, tc.dock},
			}
			for _, c := range contexts {
				if got := len(seen[c.ctx]); got != c.want {
					t.Errorf("%s bindings = %d, want %d", c.ctx, got, c.want)
				}
			}
		})
	}
	// The file counts: each child holds its parent's bindings plus its own file.
	for _, tc := range presetCounts[1:] {
		parent := "default"
		if tc.name == "macos-gui" {
			parent = "macos"
		}
		parentHeld := len(mustPreset(t, parent).Bindings())
		if got := len(mustPreset(t, tc.name).Bindings()); got != parentHeld+tc.file {
			t.Errorf("%s: held bindings = %d, want %s's %d plus its own %d", tc.name, got, parent, parentHeld, tc.file)
		}
	}
}

// TestEveryBindingIsInAConsultedContext: gvim resolves keys in the editor contexts, the
// file explorer, the terminal, and the overlay contexts whose surfaces consult the keymap
// (prompt, searchPrompt, popup, menu, settings, dock); completion and compositeBuffer are
// not resolved, so the presets must not name them.
func TestEveryBindingIsInAConsultedContext(t *testing.T) {
	for _, name := range presetNames {
		for _, b := range mustPreset(t, name).Bindings() {
			if !keymap.Consulted(b.When) {
				t.Errorf("%s binds %s in %q, which gvim does not resolve", name, b.Chord, b.When)
			}
			if b.Action == "" || b.Chord == "" {
				t.Errorf("%s has an incomplete binding: %+v", name, b)
			}
		}
	}
}

// TestPresetMenuAccelerators pins Fresh's menu keys, which every preset inherits from
// default's global context: F10 activates the menu bar and Alt+letter opens a dropdown.
// Fresh's Emacs preset overrides Alt+X with Quick Open, so it has no Explorer mnemonic,
// and gvim adds an Alt+L for its gvim-only LSP menu.
func TestPresetMenuAccelerators(t *testing.T) {
	names := map[string]string{
		"f": "File", "e": "Edit", "v": "View", "s": "Selection",
		"g": "Go", "h": "Help", "x": "Explorer", "l": "LSP",
	}
	for _, name := range presetNames {
		m := mustPreset(t, name)
		b, ok := m.Resolve("F10", keymap.ContextGlobal)
		if !ok || b.Action != "menu_activate" {
			t.Errorf("%s F10 = %+v, %v; want menu_activate", name, b, ok)
		}
		for letter, menu := range names {
			if name == "emacs" && letter == "x" {
				continue // Fresh's Emacs binds Alt+X to Quick Open
			}
			b, ok := m.Resolve("alt+"+letter, keymap.ContextGlobal)
			if !ok || b.Action != "menu_open" || b.Args["name"] != menu {
				t.Errorf("%s alt+%s = %+v, %v; want menu_open %q", name, letter, b, ok, menu)
			}
		}
	}
	// Emacs keeps its own Alt+X.
	b, ok := mustPreset(t, "emacs").Resolve("alt+x", keymap.ContextGlobal)
	if !ok || b.Action != "quick_open" {
		t.Errorf("emacs alt+x = %+v, %v; want quick_open", b, ok)
	}
}

// TestGvimExtensions pins the keys gvim adds where Fresh has none: the command palette,
// Save As and Delete Line shortcuts, the fold, split and duplicate-line keys, the
// gvim-only LSP menu and the VS Code idioms. The moved chords (toggle_fold Alt+Z,
// duplicate_line Ctrl+Shift+D and macOS Save As Alt+Shift+S) hold no Fresh binding in any
// preset or context, so they are free to use.
func TestGvimExtensions(t *testing.T) {
	tests := []struct {
		preset string
		ctx    keymap.Context
		steps  []string
		action string
	}{
		{"default", keymap.ContextGlobal, []string{"ctrl+shift+p"}, "command_palette"},
		{"default", keymap.ContextNormal, []string{"ctrl+shift+s"}, "save_as"},
		{"default", keymap.ContextNormal, []string{"ctrl+shift+k"}, "delete_line"},
		{"default", keymap.ContextNormal, []string{"ctrl+shift+d"}, "duplicate_line"},
		{"default", keymap.ContextNormal, []string{"alt+z"}, "toggle_fold"},
		{"default", keymap.ContextNormal, []string{"ctrl+backslash"}, "split_vertical"},
		{"default", keymap.ContextNormal, []string{"alt+backslash"}, "split_horizontal"},
		{"default", keymap.ContextNormal, []string{"alt+shift+w"}, "close_split"},
		{"default", keymap.ContextNormal, []string{"ctrl+shift+m"}, "toggle_diagnostics_panel"},
		{"default", keymap.ContextGlobal, []string{"alt+l"}, "menu_open"},
		{"macos", keymap.ContextNormal, []string{"alt+shift+s"}, "save_as"},
		{"macos", keymap.ContextNormal, []string{"alt+z"}, "toggle_fold"},
		{"macos", keymap.ContextGlobal, []string{"ctrl+shift+p"}, "command_palette"},
		{"macos-gui", keymap.ContextNormal, []string{"super+shift+d"}, "duplicate_line"},
		{"macos-gui", keymap.ContextNormal, []string{"super+shift+f"}, "open_live_grep"},
		{"emacs", keymap.ContextNormal, []string{"ctrl+x", "ctrl+n"}, "new"},
		{"emacs", keymap.ContextNormal, []string{"ctrl+x", "f"}, "toggle_fold"},
		{"vscode", keymap.ContextNormal, []string{"ctrl+shift+f"}, "open_live_grep"},
		{"vscode", keymap.ContextNormal, []string{"ctrl+shift+e"}, "focus_file_explorer"},
		{"vscode", keymap.ContextNormal, []string{"ctrl+k", "ctrl+l"}, "toggle_fold"},
		{"vscode", keymap.ContextNormal, []string{"ctrl+`"}, "toggle_utility_dock"},
		{"vscode", keymap.ContextNormal, []string{"ctrl+shift+backslash"}, "goto_matching_bracket"},
	}
	for _, tc := range tests {
		t.Run(tc.preset+" "+tc.steps[0], func(t *testing.T) {
			b, ok := mustPreset(t, tc.preset).ResolveSequence(tc.steps, tc.ctx)
			if !ok || b.Action != tc.action {
				t.Errorf("%v in %s = %+v, %v; want %s", tc.steps, tc.ctx, b, ok, tc.action)
			}
		})
	}
	// The gvim-only LSP menu has a name, and it is the menu the bar has.
	b, _ := mustPreset(t, "default").Resolve("alt+l", keymap.ContextGlobal)
	if b.Args["name"] != "LSP" {
		t.Errorf("alt+l menu name = %v, want LSP", b.Args["name"])
	}
}

// TestRepresentativeFreshBindings pins bindings from Fresh's keymaps for each context
// gvim resolves: block selection, multi-cursor line ends, bookmarks, shell commands, case
// transforms, line selection, query replace, line killing, scrolling, tours, macros, LSP,
// the sidebar section cycle, the dock, the file explorer and the terminal.
func TestRepresentativeFreshBindings(t *testing.T) {
	type row struct {
		preset string
		ctx    keymap.Context
		steps  []string
		action string
		args   map[string]any
	}
	tests := []row{
		// global
		{"default", keymap.ContextGlobal, []string{"ctrl+p"}, "quick_open", nil},
		{"default", keymap.ContextGlobal, []string{"ctrl+'"}, "quick_open_files", nil},
		{"default", keymap.ContextGlobal, []string{"ctrl+;"}, "quick_open_buffers", nil},
		// normal: selection, multi-cursor and the Fresh actions of the round-2 backlog
		{"default", keymap.ContextNormal, []string{"alt+shift+left"}, "block_select_left", nil},
		{"default", keymap.ContextNormal, []string{"alt+shift+right"}, "block_select_right", nil},
		{"default", keymap.ContextNormal, []string{"alt+shift+up"}, "block_select_up", nil},
		{"default", keymap.ContextNormal, []string{"alt+shift+down"}, "block_select_down", nil},
		{"default", keymap.ContextNormal, []string{"shift+alt+i"}, "add_cursors_to_line_ends", nil},
		{"default", keymap.ContextNormal, []string{"ctrl+shift+7"}, "set_bookmark", map[string]any{"char": "7"}},
		{"default", keymap.ContextNormal, []string{"alt+7"}, "jump_to_bookmark", map[string]any{"char": "7"}},
		{"default", keymap.ContextNormal, []string{"alt+|"}, "shell_command", nil},
		{"default", keymap.ContextNormal, []string{"alt+shift+|"}, "shell_command_replace", nil},
		{"default", keymap.ContextNormal, []string{"alt+u"}, "to_upper_case", nil},
		{"default", keymap.ContextNormal, []string{"alt+l"}, "to_lower_case", nil},
		{"default", keymap.ContextNormal, []string{"ctrl+l"}, "expand_line_selection", nil},
		{"default", keymap.ContextNormal, []string{"ctrl+alt+r"}, "query_replace", nil},
		{"default", keymap.ContextNormal, []string{"alt+a"}, "start_search_replace", nil},
		{"default", keymap.ContextNormal, []string{"ctrl+alt+right"}, "search_replace_next_match", nil},
		{"default", keymap.ContextNormal, []string{"ctrl+alt+left"}, "search_replace_prev_match", nil},
		{"default", keymap.ContextNormal, []string{"ctrl+k"}, "delete_to_line_end", nil},
		{"default", keymap.ContextNormal, []string{"ctrl+t"}, "new", nil},
		{"default", keymap.ContextNormal, []string{"ctrl+up"}, "scroll_up", nil},
		{"default", keymap.ContextNormal, []string{"ctrl+down"}, "scroll_down", nil},
		{"default", keymap.ContextNormal, []string{"ctrl+alt+n"}, "tour_next", nil},
		{"default", keymap.ContextNormal, []string{"ctrl+alt+p"}, "tour_prev", nil},
		{"default", keymap.ContextNormal, []string{"F4"}, "play_last_macro", nil},
		{"default", keymap.ContextNormal, []string{"F5"}, "stop_macro_recording", nil},
		{"default", keymap.ContextNormal, []string{"alt+F5"}, "compare_next_difference", nil},
		{"default", keymap.ContextNormal, []string{"shift+alt+F5"}, "compare_prev_difference", nil},
		{"default", keymap.ContextNormal, []string{"ctrl+F6"}, "compare_switch_side", nil},
		{"default", keymap.ContextNormal, []string{"ctrl+space"}, "lsp_completion", nil},
		{"default", keymap.ContextNormal, []string{"F2"}, "lsp_rename", nil},
		{"default", keymap.ContextNormal, []string{"ctrl+F12"}, "lsp_implementation", nil},
		{"default", keymap.ContextNormal, []string{"alt+."}, "lsp_code_actions", nil},
		{"default", keymap.ContextNormal, []string{"alt+k"}, "lsp_hover", nil},
		{"default", keymap.ContextNormal, []string{"shift+F1"}, "keyboard_shortcuts", nil},
		{"default", keymap.ContextNormal, []string{"alt+`"}, "open_terminal_in_dock", nil},
		{"default", keymap.ContextNormal, []string{"alt+o"}, "toggle_dock_focus", nil},
		{"default", keymap.ContextNormal, []string{"alt+shift+n"}, "focus_next_sidebar_section", nil},
		{"default", keymap.ContextNormal, []string{"alt+shift+p"}, "focus_prev_sidebar_section", nil},
		{"default", keymap.ContextNormal, []string{"alt+/"}, "open_live_grep", nil},
		{"default", keymap.ContextNormal, []string{"alt+r"}, "resume_live_grep", nil},
		{"default", keymap.ContextNormal, []string{"ctrl+b"}, "toggle_file_explorer", nil},
		{"default", keymap.ContextNormal, []string{"alt+]"}, "next_split", nil},
		{"default", keymap.ContextNormal, []string{"alt+["}, "prev_split", nil},
		{"default", keymap.ContextNormal, []string{"alt+b"}, "move_word_left", nil},
		{"default", keymap.ContextNormal, []string{"ctrl+h"}, "delete_word_backward", nil},
		{"default", keymap.ContextNormal, []string{"ctrl+delete"}, "delete_word_forward", nil},
		{"default", keymap.ContextNormal, []string{"ctrl+w"}, "select_word", nil},
		{"default", keymap.ContextNormal, []string{"alt+t"}, "popup_focus", nil},
		{"default", keymap.ContextNormal, []string{"shift+pageup"}, "select_page_up", nil},
		{"default", keymap.ContextNormal, []string{"shift+pagedown"}, "select_page_down", nil},
		{"default", keymap.ContextNormal, []string{"ctrl+shift+up"}, "select_to_paragraph_up", nil},
		{"default", keymap.ContextNormal, []string{"ctrl+shift+down"}, "select_to_paragraph_down", nil},
		{"default", keymap.ContextNormal, []string{"backtab"}, "dedent_selection", nil},
		// file_explorer
		{"default", keymap.ContextFileExplorer, []string{"down"}, "file_explorer_down", nil},
		{"default", keymap.ContextFileExplorer, []string{"ctrl+r"}, "file_explorer_refresh", nil},
		{"default", keymap.ContextFileExplorer, []string{"F2"}, "file_explorer_rename", nil},
		{"default", keymap.ContextFileExplorer, []string{"ctrl+n"}, "file_explorer_new_file", nil},
		{"default", keymap.ContextFileExplorer, []string{"ctrl+shift+n"}, "file_explorer_new_directory", nil},
		{"default", keymap.ContextFileExplorer, []string{"ctrl+i"}, "file_explorer_toggle_gitignored", nil},
		{"default", keymap.ContextFileExplorer, []string{"ctrl+o"}, "open", nil},
		{"default", keymap.ContextFileExplorer, []string{"space"}, "file_explorer_toggle_select", nil},
		{"default", keymap.ContextFileExplorer, []string{"alt+t"}, "popup_focus", nil},
		{"default", keymap.ContextFileExplorer, []string{"alt+shift+n"}, "focus_next_sidebar_section", nil},
		{"default", keymap.ContextFileExplorer, []string{"backspace"}, "file_explorer_search_backspace", nil},
		// terminal
		{"default", keymap.ContextTerminal, []string{"ctrl+]"}, "terminal_escape", nil},
		{"default", keymap.ContextTerminal, []string{"F9"}, "toggle_keyboard_capture", nil},
		{"default", keymap.ContextTerminal, []string{"ctrl+v"}, "terminal_paste", nil},
		{"default", keymap.ContextTerminal, []string{"alt+o"}, "toggle_dock_focus", nil},
		{"default", keymap.ContextTerminal, []string{"alt+shift+p"}, "focus_prev_sidebar_section", nil},
		// macos
		{"macos", keymap.ContextNormal, []string{"ctrl+r"}, "redo", nil},
		{"macos", keymap.ContextNormal, []string{"ctrl+u"}, "delete_to_line_start", nil},
		{"macos", keymap.ContextNormal, []string{"ctrl+alt+k"}, "delete_line", nil},
		{"macos", keymap.ContextNormal, []string{"ctrl+t"}, "new", nil},
		{"macos", keymap.ContextNormal, []string{"alt+0"}, "noop", nil},
		{"macos", keymap.ContextTerminal, []string{"ctrl+/"}, "toggle_keyboard_capture", nil},
		// macos-gui
		{"macos-gui", keymap.ContextNormal, []string{"super+,"}, "open_settings", nil},
		{"macos-gui", keymap.ContextNormal, []string{"super+backspace"}, "delete_to_line_start", nil},
		{"macos-gui", keymap.ContextNormal, []string{"super+]"}, "insert_tab", nil},
		{"macos-gui", keymap.ContextNormal, []string{"super+["}, "dedent_selection", nil},
		{"macos-gui", keymap.ContextTerminal, []string{"super+v"}, "terminal_paste", nil},
		{"macos-gui", keymap.ContextFileExplorer, []string{"super+b"}, "focus_editor", nil},
		// emacs
		{"emacs", keymap.ContextNormal, []string{"ctrl+x", "ctrl+s"}, "save", nil},
		{"emacs", keymap.ContextNormal, []string{"ctrl+x", "s"}, "save_all", nil},
		{"emacs", keymap.ContextNormal, []string{"ctrl+x", "k"}, "close", nil},
		{"emacs", keymap.ContextNormal, []string{"alt+%"}, "query_replace", nil},
		{"emacs", keymap.ContextNormal, []string{"alt+m"}, "smart_home", nil},
		{"emacs", keymap.ContextNormal, []string{"ctrl+space"}, "set_mark", nil},
		{"emacs", keymap.ContextNormal, []string{"alt+{"}, "move_to_paragraph_up", nil},
		{"emacs", keymap.ContextNormal, []string{"ctrl+l"}, "recenter", nil},
		{"emacs", keymap.ContextNormal, []string{"ctrl+o"}, "open_line", nil},
		{"emacs", keymap.ContextNormal, []string{"alt+/"}, "dabbrev_expand", nil},
		{"emacs", keymap.ContextNormal, []string{"alt+tab"}, "lsp_completion", nil},
		// vscode
		{"vscode", keymap.ContextNormal, []string{"ctrl+d"}, "add_cursor_next_match", nil},
		{"vscode", keymap.ContextNormal, []string{"ctrl+/"}, "toggle_comment", nil},
		{"vscode", keymap.ContextNormal, []string{"ctrl+g"}, "goto_line", nil},
		{"vscode", keymap.ContextNormal, []string{"ctrl+shift+k"}, "delete_line", nil},
	}
	for _, tc := range tests {
		t.Run(tc.preset+" "+string(tc.ctx)+" "+tc.steps[0], func(t *testing.T) {
			b, ok := mustPreset(t, tc.preset).ResolveSequence(tc.steps, tc.ctx)
			if !ok || b.Action != tc.action {
				t.Fatalf("%v in %s = %+v, %v; want %s", tc.steps, tc.ctx, b, ok, tc.action)
			}
			if len(tc.args) > 0 && !reflect.DeepEqual(b.Args, tc.args) {
				t.Fatalf("%v args = %v, want %v", tc.steps, b.Args, tc.args)
			}
		})
	}
}

// TestPresetIdioms checks that each preset keeps its own key style.
func TestPresetIdioms(t *testing.T) {
	tests := []struct {
		preset string
		steps  []string
		ctx    keymap.Context
		action string
	}{
		{"emacs", []string{"ctrl+x", "ctrl+s"}, keymap.ContextNormal, "save"},
		{"emacs", []string{"ctrl+x", "ctrl+f"}, keymap.ContextNormal, "open"},
		{"emacs", []string{"ctrl+x", "ctrl+c"}, keymap.ContextNormal, "quit"},
		{"emacs", []string{"ctrl+x", "2"}, keymap.ContextNormal, "split_horizontal"},
		{"emacs", []string{"ctrl+x", "3"}, keymap.ContextNormal, "split_vertical"},
		{"emacs", []string{"ctrl+x", "o"}, keymap.ContextNormal, "next_split"},
		{"emacs", []string{"alt+g", "g"}, keymap.ContextNormal, "goto_line"},
		{"emacs", []string{"alt+x"}, keymap.ContextGlobal, "quick_open"},
		{"vscode", []string{"ctrl+shift+f"}, keymap.ContextNormal, "open_live_grep"},
		{"vscode", []string{"ctrl+k", "ctrl+l"}, keymap.ContextNormal, "toggle_fold"},
		{"vscode", []string{"ctrl+shift+k"}, keymap.ContextNormal, "delete_line"},
		{"macos", []string{"ctrl+t"}, keymap.ContextNormal, "new"},
		{"macos", []string{"alt+shift+s"}, keymap.ContextNormal, "save_as"},
		{"macos", []string{"ctrl+-"}, keymap.ContextNormal, "navigate_back"},
		{"macos-gui", []string{"super+s"}, keymap.ContextNormal, "save"},
		{"macos-gui", []string{"super+shift+p"}, keymap.ContextGlobal, "command_palette"},
		{"macos-gui", []string{"super+d"}, keymap.ContextNormal, "add_cursor_next_match"},
		{"macos-gui", []string{"super+-"}, keymap.ContextNormal, "navigate_back"},
	}
	for _, tc := range tests {
		b, ok := mustPreset(t, tc.preset).ResolveSequence(tc.steps, tc.ctx)
		if !ok || b.Action != tc.action {
			t.Errorf("%s %v = %q (%v), want %s", tc.preset, tc.steps, b.Action, ok, tc.action)
		}
	}
}

// TestErrorJumpKeys: F8 and Shift+F8 are Fresh's error keys in the default keymap and
// every preset inherits them. Fresh's macOS preset adds Ctrl+' and Ctrl+; for the
// terminals that make F8 a media key, and keeps F8 as well.
func TestErrorJumpKeys(t *testing.T) {
	for _, name := range presetNames {
		m := mustPreset(t, name)
		if b, ok := m.Resolve("F8", keymap.ContextNormal); !ok || b.Action != "jump_to_next_error" {
			t.Errorf("%s: F8 = %q (%v), want jump_to_next_error", name, b.Action, ok)
		}
		if b, ok := m.Resolve("shift+F8", keymap.ContextNormal); !ok || b.Action != "jump_to_previous_error" {
			t.Errorf("%s: shift+F8 = %q (%v), want jump_to_previous_error", name, b.Action, ok)
		}
	}
	mac := mustPreset(t, "macos")
	for chord, want := range map[string]string{"ctrl+'": "jump_to_next_error", "ctrl+;": "jump_to_previous_error"} {
		if b, ok := mac.Resolve(chord, keymap.ContextNormal); !ok || b.Action != want {
			t.Errorf("macos %s = %q (%v), want %s", chord, b.Action, ok, want)
		}
	}
}

// TestDefaultBindingsResolveToThemselves checks that every binding of the base keymap is
// in effect: it names an action, and the binding resolves in its context to the action
// and arguments it names. A keymap action with no handler is the dead-binding test's
// concern (internal/uiadapter); this test is the file's own consistency check.
func TestDefaultBindingsResolveToThemselves(t *testing.T) {
	m := mustPreset(t, "default")
	for _, b := range m.Bindings() {
		if b.Action == keymap.ActionNoop || b.Action == keymap.ActionUnbind {
			continue
		}
		if b.Sequence == nil && m.HasSequencePrefix([]string{b.Chord}, b.When) {
			continue // a prefix never runs by itself
		}
		var got keymap.Binding
		var ok bool
		if b.Sequence != nil {
			got, ok = m.ResolveSequence(b.Sequence, b.When)
		} else {
			got, ok = m.Resolve(b.Chord, b.When)
		}
		if !ok || got.Action != b.Action || !reflect.DeepEqual(got.Args, b.Args) {
			t.Errorf("%s in %s resolves to %+v, %v; want %s %v", b.Chord, b.When, got, ok, b.Action, b.Args)
		}
	}
}
