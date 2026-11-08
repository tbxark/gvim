package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui"
)

func TestBuildModelWiresTheKeybindingStore(t *testing.T) {
	opts := userRoot(t, `{"keybindings": [{"key": "u", "modifiers": ["alt"], "action": "goto_line"}]}`)
	got, err := buildCaptured(t, opts)
	if err != nil {
		t.Fatal(err)
	}
	store := got.deps.Root.Keybindings
	if store == nil {
		t.Fatal("the keybinding store is not wired into the root ports")
	}
	if list := store.Custom(); len(list) != 1 || list[0].Chord != "alt+u" || list[0].Action != "goto_line" {
		t.Fatalf("Custom() = %+v, want the entry of the config", list)
	}
	km, err := store.Save(append(store.Custom(), tui.CustomBinding{Chord: "alt+k", Action: "save", Context: "normal"}), "default")
	if err != nil {
		t.Fatal(err)
	}
	if resolveWith(t, km, "alt+k") != "save" {
		t.Error("the saved binding does not resolve")
	}
	if text := readTestFile(t, opts.Config); !strings.Contains(text, `"alt+k"`) && !strings.Contains(text, `"k"`) {
		t.Errorf("the user config was not written: %s", text)
	}
	// A later keymap switch keeps the saved binding.
	switched, err := got.deps.SwitchKeymap("default")
	if err != nil {
		t.Fatal(err)
	}
	if resolveWith(t, switched, "alt+k") != "save" {
		t.Error("a keymap switch lost the binding the editor saved")
	}
}

func TestLoadConfigReportsKeybindingWarnings(t *testing.T) {
	path := userConfigPath(t, `{"keybindings": [
  {"key": "Weird", "modifiers": ["ctrl"], "action": "save"},
  {"key": "u", "modifiers": ["alt"], "action": "no_such_action"},
  {"key": "esc", "modifiers": ["control"], "action": "save"}
]}`)
	view, err := LoadConfig(t.TempDir(), Options{Config: path})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Warnings) != 2 ||
		!strings.Contains(view.Warnings[0], `keybindings[0]: unknown key name "Weird"`) ||
		!strings.Contains(view.Warnings[1], `keybindings[1]: unknown action "no_such_action"`) {
		t.Fatalf("warnings = %q, want the two bad entries and not the valid Fresh-form one", view.Warnings)
	}
	clean := userConfigPath(t, `{"keybindings": [{"key": "u", "modifiers": ["alt"], "action": "save"}]}`)
	view, err = LoadConfig(t.TempDir(), Options{Config: clean})
	if err != nil || len(view.Warnings) != 0 {
		t.Fatalf("a clean config warns: %q, %v", view.Warnings, err)
	}
}

func userConfigPath(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gvim", "config.json")
	if body != "" {
		writeTestFile(t, path, body)
	}
	return path
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func resolveWith(t *testing.T, km tui.Keymap, chord string) string {
	t.Helper()
	key, mods := splitKeyChord(chord)
	c, ok := km.Resolve(tui.KeyPress{Name: key, Mods: mods}, tui.KeyContextNormal)
	if !ok {
		return ""
	}
	return c.Action
}

// splitKeyChord splits a canonical chord into its key and its modifiers.
func splitKeyChord(c string) (key string, mods []string) {
	if c == "+" {
		return "+", []string{}
	}
	if strings.HasSuffix(c, "++") {
		return "+", strings.Split(strings.TrimSuffix(c, "++"), "+")
	}
	parts := strings.Split(c, "+")
	return parts[len(parts)-1], append([]string{}, parts[:len(parts)-1]...)
}
