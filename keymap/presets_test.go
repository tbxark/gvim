package keymap_test

import (
	"runtime"
	"testing"

	"github.com/tbxark/gvim/keymap"
)

func TestByNameLoadsEveryPreset(t *testing.T) {
	for _, name := range []string{"default", "macos", "macos-gui", "emacs", "vscode"} {
		m, err := keymap.ByName(name)
		if err != nil {
			t.Errorf("ByName(%q): %v", name, err)
			continue
		}
		if len(m.Bindings()) == 0 {
			t.Errorf("ByName(%q) has no bindings", name)
		}
		again, err := keymap.ByName(name)
		if err != nil || again != m {
			t.Errorf("ByName(%q) must return the cached map on repeat calls (err %v)", name, err)
		}
	}
	// A child holds its parent's bindings and its own. The exact counts are pinned in
	// TestPresetBindingCounts; here only that each child is larger than its parent.
	n := func(name string) int { return len(mustPreset(t, name).Bindings()) }
	if n("macos") <= n("default") || n("macos-gui") <= n("macos") || n("emacs") <= n("default") || n("vscode") <= n("default") {
		t.Errorf("a child preset must hold more bindings than its parent: default %d, macos %d, macos-gui %d, emacs %d, vscode %d",
			n("default"), n("macos"), n("macos-gui"), n("emacs"), n("vscode"))
	}
}

func TestByNameRejectsUnknownPreset(t *testing.T) {
	for _, name := range []string{"", "Default", "macOS", "vim", "../default"} {
		if _, err := keymap.ByName(name); err == nil {
			t.Errorf("ByName(%q) succeeded, want error", name)
		}
	}
}

func TestDefaultNameFollowsPlatform(t *testing.T) {
	want := "default"
	if runtime.GOOS == "darwin" {
		want = "macos"
	}
	if got := keymap.DefaultName(); got != want {
		t.Fatalf("DefaultName() = %q on %s, want %q", got, runtime.GOOS, want)
	}
}

// TestDefaultOnDarwinRedoIsCtrlR checks the macOS behavior Fresh's menu shows:
// Redo is Ctrl+R there, and Undo stays Ctrl+Z.
func TestDefaultOnDarwinRedoIsCtrlR(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Default resolves to the macOS keymap only on darwin")
	}
	m := keymap.Default()
	if b, ok := m.Resolve("ctrl+r", keymap.ContextNormal); !ok || b.Action != "redo" {
		t.Fatalf("ctrl+r = %+v, %v; want redo", b, ok)
	}
	if b, ok := m.Resolve("ctrl+z", keymap.ContextNormal); !ok || b.Action != "undo" {
		t.Fatalf("ctrl+z = %+v, %v; want undo", b, ok)
	}
	if b, ok := m.Primary("redo"); !ok || b.Chord != "ctrl+r" {
		t.Fatalf("Primary(redo) = %+v, %v; want ctrl+r for the menu", b, ok)
	}
	if b, ok := m.Primary("undo"); !ok || b.Chord != "ctrl+z" {
		t.Fatalf("Primary(undo) = %+v, %v; want ctrl+z", b, ok)
	}
}

func TestMacosRedoAndUndo(t *testing.T) {
	m, err := keymap.ByName("macos")
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := m.Resolve("ctrl+r", keymap.ContextNormal); !ok || b.Action != "redo" {
		t.Fatalf("macos ctrl+r = %+v, %v; want redo", b, ok)
	}
	// Ctrl+Y is inherited from default.json and still redoes: the macOS file keeps it as
	// well as Ctrl+R, as Fresh's macOS keymap does.
	if b, ok := m.Resolve("ctrl+y", keymap.ContextNormal); !ok || b.Action != "redo" {
		t.Fatalf("macos ctrl+y = %+v, %v; want inherited redo", b, ok)
	}
	if b, ok := m.Primary("redo"); !ok || b.Chord != "ctrl+r" {
		t.Fatalf("macos Primary(redo) = %+v, %v; want ctrl+r", b, ok)
	}
	if b, ok := mustPreset(t, "default").Primary("redo"); !ok || b.Chord != "ctrl+y" {
		t.Fatalf("default Primary(redo) = %+v, %v; want ctrl+y", b, ok)
	}
}

// TestMacosRebindsAreVisibleOnlyInNormalContext checks Fresh's macOS preset: it overrides
// the default meanings with its own Ctrl/Option keys, and its file_explorer and terminal
// contexts keep their own meanings.
func TestMacosRebindsAreVisibleOnlyInNormalContext(t *testing.T) {
	m, err := keymap.ByName("macos")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		chord string
		ctx   keymap.Context
		want  string
	}{
		{"ctrl+a", keymap.ContextNormal, "move_line_start"},
		{"ctrl+e", keymap.ContextNormal, "move_line_end"},
		{"ctrl+d", keymap.ContextNormal, "add_cursor_next_match"},
		{"ctrl+k", keymap.ContextNormal, "delete_to_line_end"},
		{"ctrl+alt+k", keymap.ContextNormal, "delete_line"},
		{"ctrl+w", keymap.ContextNormal, "select_word"},
		{"alt+w", keymap.ContextNormal, "close_tab"},
		{"alt+shift+s", keymap.ContextNormal, "save_as"},
		{"alt+f", keymap.ContextNormal, "move_word_right"},
		{"ctrl+u", keymap.ContextNormal, "delete_to_line_start"},
		{"ctrl+t", keymap.ContextNormal, "command_palette"},
		{"ctrl+.", keymap.ContextNormal, "lsp_goto_definition"},
		{"ctrl+,", keymap.ContextNormal, "lsp_references"},
		{"ctrl+alt+r", keymap.ContextNormal, "lsp_rename"},
		{"ctrl+'", keymap.ContextNormal, "jump_to_next_error"},
		{"ctrl+;", keymap.ContextNormal, "jump_to_previous_error"},
		{"F8", keymap.ContextNormal, "jump_to_next_error"},
		{"shift+F8", keymap.ContextNormal, "jump_to_previous_error"},
		{"ctrl+e", keymap.ContextFileExplorer, "focus_editor"},
		{"ctrl+b", keymap.ContextFileExplorer, "focus_editor"},
		{"ctrl+b", keymap.ContextNormal, "toggle_file_explorer"},
		{"ctrl+g", keymap.ContextNormal, "find_next"},
		{"ctrl+alt+g", keymap.ContextNormal, "find_previous"},
		{"ctrl+l", keymap.ContextNormal, "goto_line"},
		{"ctrl+-", keymap.ContextNormal, "navigate_back"},
		{"ctrl+=", keymap.ContextNormal, "navigate_forward"},
		{"ctrl+backslash", keymap.ContextTerminal, "toggle_keyboard_capture"},
	}
	for _, tc := range tests {
		b, ok := m.Resolve(tc.chord, tc.ctx)
		if !ok || b.Action != tc.want {
			t.Errorf("macos %s/%s = %+v, %v; want %s", tc.ctx, tc.chord, b, ok, tc.want)
		}
	}
	// The default keymap keeps the non-macOS meanings.
	base, err := keymap.ByName("default")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := base.Resolve("ctrl+a", keymap.ContextNormal); b.Action != "select_all" {
		t.Errorf("default ctrl+a = %q, want select_all", b.Action)
	}
	if b, _ := base.Resolve("ctrl+g", keymap.ContextNormal); b.Action != "goto_line" {
		t.Errorf("default ctrl+g = %q, want goto_line", b.Action)
	}
	if b, _ := base.Resolve("ctrl+w", keymap.ContextNormal); b.Action != "select_word" {
		t.Errorf("default ctrl+w = %q, want select_word", b.Action)
	}
}

func TestInheritanceChain(t *testing.T) {
	gui, err := keymap.ByName("macos-gui")
	if err != nil {
		t.Fatal(err)
	}
	// macos-gui inherits macos, which inherits default. It adds the Cmd chords of a GUI
	// and keeps the Ctrl chords it inherits, as Fresh's macos-gui does: Fresh's macos-gui
	// no longer disables Ctrl (a gvim-specific pruning of the first port did).
	if b, ok := gui.Resolve("super+p", keymap.ContextGlobal); !ok || b.Action != "quick_open" {
		t.Errorf("macos-gui super+p = %+v, %v; want quick_open", b, ok)
	}
	if b, ok := gui.Resolve("ctrl+p", keymap.ContextGlobal); !ok || b.Action != "quick_open" {
		t.Errorf("macos-gui ctrl+p = %+v, %v; want the inherited quick_open", b, ok)
	}
	if b, ok := gui.Resolve("ctrl+r", keymap.ContextNormal); !ok || b.Action != "redo" {
		t.Errorf("macos-gui ctrl+r = %+v, %v; want the macos redo", b, ok)
	}
	if b, ok := gui.Resolve("super+s", keymap.ContextNormal); !ok || b.Action != "save" {
		t.Errorf("macos-gui super+s = %+v, %v; want save", b, ok)
	}
	// The menu shows the preset's own binding before an inherited one (AK-08), so the GUI
	// keymap shows Cmd+S and not the Ctrl+S it inherits.
	if b, ok := gui.Primary("save"); !ok || b.Chord != "super+s" {
		t.Errorf("macos-gui Primary(save) = %+v, %v; want super+s", b, ok)
	}

	vs, err := keymap.ByName("vscode")
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := vs.Resolve("ctrl+d", keymap.ContextNormal); !ok || b.Action != "add_cursor_next_match" {
		t.Errorf("vscode ctrl+d = %+v, %v; want add_cursor_next_match", b, ok)
	}
	if b, ok := vs.Resolve("ctrl+s", keymap.ContextNormal); !ok || b.Action != "save" {
		t.Errorf("vscode ctrl+s = %+v, %v; want inherited save", b, ok)
	}
}

func TestEmacsSequences(t *testing.T) {
	em, err := keymap.ByName("emacs")
	if err != nil {
		t.Fatal(err)
	}
	b, ok := em.ResolveSequence([]string{"ctrl+x", "ctrl+s"}, keymap.ContextNormal)
	if !ok || b.Action != "save" || b.Chord != "ctrl+x ctrl+s" {
		t.Fatalf("C-x C-s = %+v, %v; want save", b, ok)
	}
	if got := b.Sequence; len(got) != 2 || got[0] != "ctrl+x" || got[1] != "ctrl+s" {
		t.Fatalf("Sequence = %v, want [ctrl+x ctrl+s]", got)
	}
	// Fresh's Emacs preset binds C-x s to Save All and C-x C-c to Quit.
	if b, ok := em.ResolveSequence([]string{"Ctrl+X", "S"}, keymap.ContextNormal); !ok || b.Action != "save_all" {
		t.Fatalf("C-x s = %+v, %v; want save_all", b, ok)
	}
	if b, ok := em.ResolveSequence([]string{"ctrl+x", "ctrl+c"}, keymap.ContextNormal); !ok || b.Action != "quit" {
		t.Fatalf("C-x C-c = %+v, %v; want quit", b, ok)
	}
	if b, ok := em.ResolveSequence([]string{"alt+g", "g"}, keymap.ContextNormal); !ok || b.Action != "goto_line" {
		t.Fatalf("M-g g = %+v, %v; want goto_line", b, ok)
	}
	if b, ok := em.ResolveSequence([]string{"alt+g", "alt+g"}, keymap.ContextNormal); !ok || b.Action != "goto_line" {
		t.Fatalf("M-g M-g = %+v, %v; want goto_line", b, ok)
	}
	// A sequence is not a single chord: Resolve must not match it.
	if _, ok := em.Resolve("ctrl+x ctrl+s", keymap.ContextNormal); ok {
		t.Fatal("Resolve must not match a multi-key binding")
	}
	// Fresh's Emacs file overrides the default global Ctrl+P (Quick Open) with noop and
	// binds M-x to Quick Open. The Ctrl+Z, Ctrl+Q and Ctrl+O the older gvim preset
	// disabled are Fresh bindings and stay live.
	if b, ok := em.Resolve("ctrl+p", keymap.ContextGlobal); !ok || b.Action != "noop" {
		t.Fatalf("emacs ctrl+p = %+v, %v; want noop override", b, ok)
	}
	if b, ok := em.Resolve("alt+x", keymap.ContextGlobal); !ok || b.Action != "quick_open" {
		t.Fatalf("emacs alt+x = %+v, %v; want quick_open", b, ok)
	}
	if b, ok := em.Resolve("ctrl+z", keymap.ContextNormal); !ok || b.Action != "undo" {
		t.Fatalf("emacs ctrl+z = %+v, %v; want the inherited undo", b, ok)
	}
	if b, ok := em.Resolve("ctrl+q", keymap.ContextNormal); !ok || b.Action != "quit" {
		t.Fatalf("emacs ctrl+q = %+v, %v; want the inherited quit", b, ok)
	}
	if b, ok := em.Resolve("ctrl+/", keymap.ContextNormal); !ok || b.Action != "undo" {
		t.Fatalf("emacs ctrl+/ = %+v, %v; want undo", b, ok)
	}
	if b, ok := em.Resolve("ctrl+w", keymap.ContextNormal); !ok || b.Action != "cut" {
		t.Fatalf("emacs ctrl+w = %+v, %v; want cut", b, ok)
	}
	if _, ok := em.ResolveSequence(nil, keymap.ContextNormal); ok {
		t.Fatal("empty sequence must not resolve")
	}
}

func TestHasSequencePrefix(t *testing.T) {
	em, err := keymap.ByName("emacs")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		steps []string
		ctx   keymap.Context
		want  bool
	}{
		{"first step of C-x C-s", []string{"ctrl+x"}, keymap.ContextNormal, true},
		{"normalized spelling", []string{"Ctrl+X"}, keymap.ContextNormal, true},
		{"first step of M-g g", []string{"alt+g"}, keymap.ContextNormal, true},
		{"a complete sequence is not a prefix", []string{"ctrl+x", "ctrl+s"}, keymap.ContextNormal, false},
		{"an unrelated key", []string{"ctrl+s"}, keymap.ContextNormal, false},
		{"a wrong second step", []string{"ctrl+x", "ctrl+z"}, keymap.ContextNormal, false},
		{"another context", []string{"ctrl+x"}, keymap.ContextFileExplorer, false},
		{"no steps", nil, keymap.ContextNormal, false},
		{"not a key", []string{"bogus+"}, keymap.ContextNormal, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := em.HasSequencePrefix(tt.steps, tt.ctx); got != tt.want {
				t.Errorf("HasSequencePrefix(%v, %s) = %v, want %v", tt.steps, tt.ctx, got, tt.want)
			}
		})
	}
	if keymap.Default().HasSequencePrefix([]string{"ctrl+x"}, keymap.ContextNormal) {
		t.Error("the default keymap has no sequence, yet ctrl+x is a prefix")
	}
}

func TestLoadInheritance(t *testing.T) {
	src := `{"inherits":"default","bindings":[
		{"key":"q","modifiers":["ctrl"],"action":"custom","when":"normal"},
		{"key":"s","modifiers":["ctrl"],"action":"other","when":"normal"}]}`
	m, err := keymap.Load([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := m.Resolve("ctrl+p", keymap.ContextGlobal); !ok || b.Action != "quick_open" {
		t.Fatalf("inherited ctrl+p = %+v, %v; want quick_open", b, ok)
	}
	if b, ok := m.Resolve("ctrl+q", keymap.ContextNormal); !ok || b.Action != "custom" {
		t.Fatalf("own ctrl+q = %+v, %v; want custom", b, ok)
	}
	if b, ok := m.Resolve("ctrl+s", keymap.ContextNormal); !ok || b.Action != "other" {
		t.Fatalf("own ctrl+s = %+v, %v; want override of save", b, ok)
	}
	if got, want := len(m.Bindings()), len(mustPreset(t, "default").Bindings())+2; got != want {
		t.Fatalf("bindings = %d, want the parent's plus 2 (%d)", got, want)
	}
}

func TestLoadRejectsUnknownParent(t *testing.T) {
	if _, err := keymap.Load([]byte(`{"inherits":"nope","bindings":[]}`)); err == nil {
		t.Fatal("unknown parent must fail")
	}
}

func TestLoadRejectsBadSequenceShapes(t *testing.T) {
	bad := map[string]string{
		"key and keys":  `{"bindings":[{"key":"a","modifiers":[],"keys":[{"key":"b","modifiers":[]}],"action":"x"}]}`,
		"neither":       `{"bindings":[{"modifiers":[],"action":"x"}]}`,
		"bad step mod":  `{"bindings":[{"keys":[{"key":"a","modifiers":["hyper"]}],"action":"x"}]}`,
		"empty action":  `{"bindings":[{"key":"a","modifiers":[],"action":""}]}`,
		"empty keys[]":  `{"bindings":[{"keys":[],"action":"x"}]}`,
		"unknown inner": `{"bindings":[{"keys":[{"key":"a"},{"key":"Wibble"}],"action":"x"}]}`,
	}
	for name, src := range bad {
		if _, err := keymap.Load([]byte(src)); err == nil {
			t.Errorf("%s: Load succeeded, want error", name)
		}
	}
}

// TestSuperModifier checks the modifier macos-gui uses. Canonical order is
// ctrl, alt, shift, super; the TUI never produces super, but the file must load.
func TestSuperModifier(t *testing.T) {
	got, err := keymap.Canonical("s", []string{"super", "shift"})
	if err != nil || got != "shift+super+s" {
		t.Fatalf("Canonical(s, super+shift) = %q, %v; want shift+super+s", got, err)
	}
	got, err = keymap.Normalize("Super+S")
	if err != nil || got != "super+s" {
		t.Fatalf("Normalize(Super+S) = %q, %v; want super+s", got, err)
	}
	if _, err := keymap.Canonical("s", []string{"hyper"}); err == nil {
		t.Fatal("hyper must stay unknown")
	}
}

func mustPreset(t *testing.T, name string) *keymap.Map {
	t.Helper()
	m, err := keymap.ByName(name)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestFileExplorerAliasIsFolded(t *testing.T) {
	m := keymap.Default()
	// Fresh's default.json spells some explorer bindings "fileExplorer"; they must
	// resolve in the file_explorer context.
	b, ok := m.Resolve("ctrl+o", keymap.ContextFileExplorer)
	if !ok || b.Action != "open" || b.When != keymap.ContextFileExplorer {
		t.Fatalf("ctrl+o in explorer = %+v, %v; want open in file_explorer", b, ok)
	}
}
