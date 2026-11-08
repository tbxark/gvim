package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/keymap"
)

// openNamed opens one file per name, in order, so the last name is the active tab.
// Each file holds its own name as text.
func openNamed(t *testing.T, ed *editor.Editor, names ...string) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		path := filepath.Join(dir, n)
		if err := os.WriteFile(path, []byte("text of "+n+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := ed.OpenFile(path); err != nil {
			t.Fatalf("OpenFile(%s): %v", n, err)
		}
	}
}

// TestDocAtGivesEachTabItsDocument checks the tab documents that split panes paint,
// including the untyped nil for a tab that is not there.
func TestDocAtGivesEachTabItsDocument(t *testing.T) {
	t.Parallel()
	ed := editor.New(editor.Options{})
	openNamed(t, ed, "a.txt", "b.txt", "c.txt")
	a := editorAdapter{ed: ed, spans: &spanCache{}, others: map[*editor.Document]*spanCache{}}
	for i, want := range []string{"a.txt", "b.txt", "c.txt"} {
		d := a.DocAt(i)
		if d == nil || d.Name() != want {
			t.Fatalf("DocAt(%d) = %v, want the document %s", i, d, want)
		}
	}
	for _, i := range []int{-1, 3} {
		if d := a.DocAt(i); d != nil {
			t.Fatalf("DocAt(%d) = %T, want an untyped nil", i, d)
		}
	}
}

// TestDocAtBoundsTheSpanCachesToOpenTabs checks that the caches of closed tabs are
// dropped, so a closed document is not kept alive by the adapter.
func TestDocAtBoundsTheSpanCachesToOpenTabs(t *testing.T) {
	t.Parallel()
	ed := editor.New(editor.Options{})
	openNamed(t, ed, "a.txt", "b.txt", "c.txt")
	a := editorAdapter{ed: ed, spans: &spanCache{}, others: map[*editor.Document]*spanCache{}}
	a.DocAt(0)
	a.DocAt(1)
	if got := len(a.others); got != 2 {
		t.Fatalf("caches after two other tabs = %d, want 2", got)
	}
	for range 2 {
		if err := ed.CloseTab(0, false); err != nil {
			t.Fatalf("CloseTab: %v", err)
		}
	}
	openNamed(t, ed, "d.txt")
	if d := a.DocAt(0); d == nil || d.Name() != "c.txt" {
		t.Fatalf("DocAt(0) = %v, want c.txt", d)
	}
	if got := len(a.others); got != 1 {
		t.Fatalf("caches with one open non-active tab = %d, want 1", got)
	}
}

// TestKeymapAdapterListsItsBindings checks the help and keybinding views read the
// keymap's bindings as the root expects them.
func TestKeymapAdapterListsItsBindings(t *testing.T) {
	t.Parallel()
	km := keymap.Default()
	k := keymapAdapter{km: km}
	got, want := k.Bindings(), km.Bindings()
	if len(got) != len(want) {
		t.Fatalf("Bindings() has %d entries, want %d", len(got), len(want))
	}
	for i, b := range want {
		if w := (tui.KeyBinding{Action: b.Action, Chord: b.Chord, Context: string(b.When)}); got[i] != w {
			t.Fatalf("Bindings()[%d] = %+v, want %+v", i, got[i], w)
		}
	}
	if p, ok := k.Primary("save"); !ok || p.Action != "save" || p.Chord == "" {
		t.Fatalf("Primary(save) = %+v, %v; want a chord for save", p, ok)
	}
	if _, ok := k.Primary("no_such_action"); ok {
		t.Fatal("Primary(no_such_action) found a binding")
	}
}

// TestSettingsForPrefersFlagsOverConfig checks the values open_settings lists.
func TestSettingsForPrefersFlagsOverConfig(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Theme = "dark"
	cfg.Keymap = "vscode"
	tests := []struct {
		name                  string
		opts                  Options
		wantTheme, wantKeymap string
	}{
		{"config values without flags", Options{}, "dark", "vscode"},
		{"flags win over config", Options{Theme: "light", Keymap: "emacs"}, "light", "emacs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := settingsFor(tt.opts, cfg)
			if s.Theme != tt.wantTheme || s.Keymap != tt.wantKeymap {
				t.Fatalf("settingsFor = theme %q keymap %q, want %q %q", s.Theme, s.Keymap, tt.wantTheme, tt.wantKeymap)
			}
		})
	}
	if s := settingsFor(Options{Config: "/tmp/x.json"}, config.Defaults()); s.ConfigFile != "/tmp/x.json" {
		t.Fatalf("ConfigFile = %q, want the --config path", s.ConfigFile)
	}
	if s := settingsFor(Options{}, config.Defaults()); s.Keymap != keymap.DefaultName() {
		t.Fatalf("Keymap without a name = %q, want %q", s.Keymap, keymap.DefaultName())
	}
}

// TestSettingsForCarriesTheSaveOptions checks the settings only gvim.config.get reads.
func TestSettingsForCarriesTheSaveOptions(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.LineEnding = config.LineEndingCRLF
	cfg.TrimTrailingWhitespace = true
	cfg.EnsureFinalNewline = true
	s := settingsFor(Options{}, cfg)
	if s.LineEnding != "crlf" || !s.TrimTrailingWhitespace || !s.EnsureFinalNewline {
		t.Fatalf("settingsFor = %q %v %v, want crlf true true", s.LineEnding, s.TrimTrailingWhitespace, s.EnsureFinalNewline)
	}
}

// TestSwitchKeymapKeepsTheConfigKeybindings checks that a keymap switch applies the
// config overrides, so a binding the user removed stays removed.
func TestSwitchKeymapKeepsTheConfigKeybindings(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Keybindings = json.RawMessage(`{"bindings":[{"key":"s","modifiers":["ctrl"],"action":"noop","when":"normal"}]}`)
	switchTo := switchKeymapFor(cfg)
	km, err := switchTo("emacs")
	if err != nil || km == nil {
		t.Fatalf("switch to emacs = %v, %v; want a keymap", km, err)
	}
	if cmd, ok := km.Resolve(tui.KeyPress{Name: "s", Mods: []string{"ctrl"}}, tui.KeyContextNormal); ok && cmd.Action != "noop" {
		t.Fatalf("ctrl+s resolves to %q after the switch; the config override should unbind it", cmd.Action)
	}
	if _, err := switchTo("no-such-keymap"); err == nil {
		t.Fatal("switch to an unknown keymap did not fail")
	}
}
