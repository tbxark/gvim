package keymap_test

import (
	"testing"

	"github.com/tbxark/gvim/keymap"
)

// TestMenuMnemonic pins the letter a menu label underlines. Fresh's menu accelerators are
// Alt+<letter> bindings that open a menu by name, and the label underlines the letter the
// menu's binding names.
func TestMenuMnemonic(t *testing.T) {
	m := mustPreset(t, "default")
	tests := map[string]rune{
		"File": 'f', "Edit": 'e', "View": 'v', "Selection": 's',
		"Go": 'g', "Help": 'h', "Explorer": 'x', "LSP": 'l',
	}
	for name, want := range tests {
		if got, ok := m.MenuMnemonic(name); !ok || got != want {
			t.Errorf("MenuMnemonic(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}
	// The menu name is matched without case.
	for _, name := range []string{"file", "FILE", "fIlE"} {
		if got, ok := m.MenuMnemonic(name); !ok || got != 'f' {
			t.Errorf("MenuMnemonic(%q) = %q, %v; want f", name, got, ok)
		}
	}
	for _, name := range []string{"", "NoSuchMenu", "File "} {
		if got, ok := m.MenuMnemonic(name); ok {
			t.Errorf("MenuMnemonic(%q) = %q; want no mnemonic", name, got)
		}
	}
}

// TestMenuMnemonicPerPreset: every preset inherits the accelerators from default, except
// that Fresh's Emacs preset binds Alt+X to Quick Open instead of the Explorer menu.
func TestMenuMnemonicPerPreset(t *testing.T) {
	for _, name := range presetNames {
		m := mustPreset(t, name)
		for menu, want := range map[string]rune{"File": 'f', "Edit": 'e', "View": 'v', "Selection": 's', "Go": 'g', "Help": 'h'} {
			if got, ok := m.MenuMnemonic(menu); !ok || got != want {
				t.Errorf("%s MenuMnemonic(%q) = %q, %v; want %q", name, menu, got, ok, want)
			}
		}
		_, wantExplorer := m.MenuMnemonic("Explorer")
		if name == "emacs" {
			if wantExplorer {
				t.Error("emacs has an Explorer mnemonic, but its Alt+X is Quick Open")
			}
			continue
		}
		if !wantExplorer {
			t.Errorf("%s has no Explorer mnemonic", name)
		}
	}
}

// TestMenuMnemonicIsShadowedButReported: a normal-context binding of the accelerator wins
// over the global menu binding (Fresh behaves this way), and the mnemonic still reports the
// letter so the label underlines it.
func TestMenuMnemonicIsShadowedButReported(t *testing.T) {
	m := mustPreset(t, "default")
	if b, ok := m.Resolve("alt+l", keymap.ContextNormal); !ok || b.Action != "to_lower_case" {
		t.Fatalf("default alt+l in normal = %+v, %v; want to_lower_case", b, ok)
	}
	if letter, ok := m.MenuMnemonic("LSP"); !ok || letter != 'l' {
		t.Errorf("MenuMnemonic(LSP) = %q, %v; want l even while to_lower_case shadows the chord", letter, ok)
	}
	if b, ok := m.Resolve("alt+l", keymap.ContextFileExplorer); !ok || b.Action != "menu_open" {
		t.Errorf("alt+l in the explorer = %+v, %v; want menu_open", b, ok)
	}
}

// TestMenuMnemonicFollowsOverrides: a child keymap or a user binding that replaces the
// menu's accelerator replaces or removes its mnemonic.
func TestMenuMnemonicFollowsOverrides(t *testing.T) {
	child, err := keymap.Load([]byte(`{"inherits":"default","bindings":[
		{"key":"f","modifiers":["alt"],"action":"save","when":"global"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if letter, ok := child.MenuMnemonic("File"); ok {
		t.Errorf("MenuMnemonic(File) = %q after Alt+F was rebound; want none", letter)
	}
	// A user binding of another chord for the same menu is the mnemonic: the lookup prefers
	// the later binding, as the user's bindings win over the preset.
	user, warnings := mustPreset(t, "default").WithUserBindings([]byte(`[{"key":"b","modifiers":["alt"],"action":"menu_open","args":{"name":"Go"},"when":"global"}]`), nil)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %q", warnings)
	}
	if letter, ok := user.MenuMnemonic("Go"); !ok || letter != 'b' {
		t.Errorf("MenuMnemonic(Go) = %q, %v after the user bound Alt+B; want b", letter, ok)
	}
}
