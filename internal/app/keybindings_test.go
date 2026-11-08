package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/config"
	"github.com/tbxark/gvim/internal/keymap"
	"github.com/tbxark/gvim/internal/pluginapi"
	"github.com/tbxark/gvim/internal/ui"
)

// userConfigPath returns a user config file with body in a temporary folder.
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

func TestKeybindingStoreSavesTheDocumentedForm(t *testing.T) {
	path := userConfigPath(t, `{
  // my settings
  "tab_size": 2, // narrow
  "keybindings": [
    { "key": "u", "modifiers": ["alt"], "action": "to_upper_case", "when": "normal" },
  ],
}`)
	cfg, diags, err := config.Load(config.Options{UserFile: path})
	if err != nil || len(diags) != 0 {
		t.Fatalf("Load = %v, %v", diags, err)
	}
	store := newKeybindingStore(path, cfg)
	if got := store.Custom(); len(got) != 1 || got[0].Chord != "alt+u" || got[0].Action != "to_upper_case" || got[0].Context != "normal" {
		t.Fatalf("Custom() = %+v", got)
	}
	list := []ui.CustomBinding{
		{Chord: "alt+u", Action: "to_upper_case", Context: "normal"},
		{Chord: "ctrl+s", Action: "unbind", Context: "normal"},
		{Chord: "ctrl+k", Action: "save", Context: "normal"},
		{Chord: "f12", Action: "lsp_goto_definition", Context: "global"},
		{Chord: "ctrl+x ctrl+s", Action: "save_all", Context: "normal"},
	}
	km, err := store.Save(list, "default")
	if err != nil {
		t.Fatal(err)
	}
	// The file holds the list in the documented form and keeps the rest.
	text := readTestFile(t, path)
	for _, want := range []string{"// my settings", `"tab_size": 2`, "// narrow", `"keybindings"`} {
		if !strings.Contains(text, want) {
			t.Errorf("config.json lost %q:\n%s", want, text)
		}
	}
	reload, diags, err := config.Load(config.Options{UserFile: path})
	if err != nil || len(diags) != 0 {
		t.Fatalf("the saved file does not load: %v, %v\n%s", diags, err, text)
	}
	var entries []map[string]any
	if err := json.Unmarshal(reload.Keybindings, &entries); err != nil {
		t.Fatalf("keybindings is not a list: %v\n%s", err, reload.Keybindings)
	}
	if len(entries) != 5 {
		t.Fatalf("saved %d entries, want 5:\n%s", len(entries), reload.Keybindings)
	}
	first := entries[2]
	if first["key"] != "k" || first["action"] != "save" || first["when"] != "normal" {
		t.Errorf("entry = %v, want the documented key/modifiers/action/when", first)
	}
	if mods, _ := first["modifiers"].([]any); len(mods) != 1 || mods[0] != "ctrl" {
		t.Errorf("modifiers = %v", first["modifiers"])
	}
	if f12 := entries[3]; f12["key"] != "F12" {
		t.Errorf("a function key is written %v, want F12", f12["key"])
	}
	if seq, ok := entries[4]["keys"].([]any); !ok || len(seq) != 2 {
		t.Errorf("sequence entry = %v, want keys with two steps", entries[4])
	}
	// The keymap it returns has the changes, and the next session reads the same.
	got := resolveWith(t, km, "ctrl+k")
	if got != "save" {
		t.Errorf("ctrl+k = %q after the save, want save", got)
	}
	if got := resolveWith(t, km, "ctrl+s"); got != "" {
		t.Errorf("ctrl+s = %q after the unbind, want unbound", got)
	}
	next := keymapWithOverrides(keymapMust(t, "default"), reload)
	if b, ok := next.Resolve("ctrl+k", keymap.ContextNormal); !ok || b.Action != "save" {
		t.Errorf("the next session resolves ctrl+k to %q", b.Action)
	}
	// The live config the keymap builders read has the new list.
	if live := store.current(cfg); string(live.Keybindings) == string(cfg.Keybindings) {
		t.Error("the live config still has the old keybindings")
	}
}

func resolveWith(t *testing.T, km ui.Keymap, chord string) string {
	t.Helper()
	key, mods := splitChord(chord)
	c, ok := km.Resolve(ui.KeyPress{Name: key, Mods: mods}, ui.KeyContextNormal)
	if !ok {
		return ""
	}
	return c.Action
}

func keymapMust(t *testing.T, name string) *keymap.Map {
	t.Helper()
	km, err := keymap.ByName(name)
	if err != nil {
		t.Fatal(err)
	}
	return km
}

func TestKeybindingStoreCreatesTheFileAndEmptiesTheList(t *testing.T) {
	path := userConfigPath(t, "")
	store := newKeybindingStore(path, config.Config{})
	if _, err := store.Save([]ui.CustomBinding{{Chord: "ctrl+k", Action: "save", Context: "normal"}}, "default"); err != nil {
		t.Fatal(err)
	}
	if text := readTestFile(t, path); !strings.Contains(text, `"keybindings"`) || !strings.Contains(text, `"action": "save"`) {
		t.Fatalf("file = %q", text)
	}
	if _, err := store.Save(nil, "default"); err != nil {
		t.Fatal(err)
	}
	cfg, diags, err := config.Load(config.Options{UserFile: path})
	if err != nil || len(diags) != 0 || cfg.Keybindings != nil {
		t.Fatalf("an emptied list: %v, %v, keybindings %s", diags, err, cfg.Keybindings)
	}
}

func TestKeybindingStoreRefusesWhatWouldNotApply(t *testing.T) {
	tests := []struct {
		name string
		list []ui.CustomBinding
		want string
	}{
		{"unknown action", []ui.CustomBinding{{Chord: "ctrl+k", Action: "no_such_action", Context: "normal"}}, "unknown action"},
		{"unknown context", []ui.CustomBinding{{Chord: "ctrl+k", Action: "save", Context: "nowhere"}}, "unknown when clause"},
		{"unreadable chord", []ui.CustomBinding{{Chord: "ctrl+wibble", Action: "save", Context: "normal"}}, "unknown key name"},
		{"same key twice", []ui.CustomBinding{{Chord: "ctrl+k", Action: "save", Context: "normal"}, {Chord: "ctrl+k", Action: "quit", Context: "normal"}}, "already bound"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := userConfigPath(t, `{"tab_size": 3}`)
			store := newKeybindingStore(path, config.Config{})
			if _, err := store.Save(tt.list, "default"); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Save error = %v, want %q", err, tt.want)
			}
			if got := readTestFile(t, path); got != `{"tab_size": 3}` {
				t.Errorf("the file was changed: %q", got)
			}
		})
	}
}

func TestKeybindingStoreNeverOverwritesAFileItCannotRead(t *testing.T) {
	path := userConfigPath(t, `{"tab_size": `)
	store := newKeybindingStore(path, config.Config{})
	_, err := store.Save([]ui.CustomBinding{{Chord: "ctrl+k", Action: "save", Context: "normal"}}, "default")
	if err == nil || !strings.Contains(err.Error(), "not changed") {
		t.Fatalf("Save error = %v, want it to say the file was not changed", err)
	}
	if got := readTestFile(t, path); got != `{"tab_size": ` {
		t.Errorf("the broken file was overwritten: %q", got)
	}
}

func TestKeybindingWarnings(t *testing.T) {
	cfg := config.Config{Keybindings: json.RawMessage(`[{"key":"u","modifiers":["alt"],"action":"unknown_action_xyz"},{"key":"k","modifiers":["alt"],"action":"save"}]`)}
	got := keybindingWarnings(cfg)
	if len(got) != 1 || !strings.Contains(got[0], `keybindings[0]: unknown action "unknown_action_xyz"`) {
		t.Fatalf("warnings = %q", got)
	}
	if keybindingWarnings(config.Config{}) != nil {
		t.Error("no keybindings gave warnings")
	}
	for _, tc := range []struct {
		warnings []string
		want     string
	}{
		{nil, ""},
		{[]string{"a"}, "config: a"},
		{[]string{"a", "b", "c"}, "config: 3 problems (Show Warnings lists them): a"},
	} {
		if got := configNotice(tc.warnings); got != tc.want {
			t.Errorf("configNotice(%q) = %q, want %q", tc.warnings, got, tc.want)
		}
	}
}

// The real startup wires the store into the model, so the keybinding editor can save: the
// first version of the wiring was overwritten by the construction of the root ports.
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
	km, err := store.Save(append(store.Custom(), ui.CustomBinding{Chord: "alt+k", Action: "save", Context: "normal"}), "default")
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

// Preview gives the keymap of a pending list for the keybinding editor, and writes nothing.
func TestKeybindingStorePreviewWritesNothing(t *testing.T) {
	path := userConfigPath(t, `{"tab_size": 3}`)
	store := newKeybindingStore(path, config.Config{})
	km, err := store.Preview([]ui.CustomBinding{{Chord: "ctrl+k", Action: "save", Context: "normal"}}, "default")
	if err != nil {
		t.Fatal(err)
	}
	if got := resolveWith(t, km, "ctrl+k"); got != "save" {
		t.Errorf("ctrl+k in the preview = %q, want save", got)
	}
	if got := readTestFile(t, path); got != `{"tab_size": 3}` {
		t.Errorf("Preview wrote the file: %q", got)
	}
	if len(store.Custom()) != 0 {
		t.Errorf("Preview changed the store: %+v", store.Custom())
	}
	if _, err := store.Preview([]ui.CustomBinding{{Chord: "ctrl+k", Action: "no_such_action", Context: "normal"}}, "default"); err == nil {
		t.Error("Preview accepted an unknown action")
	}
}

// config show is the command that inspects the effective configuration, so it reports the
// keybinding problems that the editor shows in its status row.
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

// TestUnknownActionDoesNotBlockEditorChanges: one entry with an unknown action is a warning. The
// editor's list leaves it out, so an edit still applies and saves, and the entry stays in the
// file for the user to fix. Before, the entry was listed, and every Preview and Save failed with
// "unknown action" until the user could not remove it.
func TestUnknownActionDoesNotBlockEditorChanges(t *testing.T) {
	path := userConfigPath(t, `{"keybindings": [
  { "key": "u", "modifiers": ["alt"], "action": "no_such_action", "when": "normal" }
]}`)
	cfg, _, err := config.Load(config.Options{UserFile: path})
	if err != nil {
		t.Fatal(err)
	}
	store := newKeybindingStore(path, cfg)
	if got := store.Custom(); len(got) != 0 {
		t.Fatalf("Custom() = %+v, want no entry: the unknown action is not an editor row", got)
	}
	list := []ui.CustomBinding{{Chord: "ctrl+k", Action: "save", Context: "normal"}}
	if _, err := store.Preview(list, "default"); err != nil {
		t.Fatalf("Preview with a valid binding: %v", err)
	}
	if _, err := store.Save(list, "default"); err != nil {
		t.Fatalf("Save with a valid binding: %v", err)
	}
	text := readTestFile(t, path)
	if !strings.Contains(text, "no_such_action") {
		t.Errorf("the save dropped the entry the user has to fix:\n%s", text)
	}
	reload, _, err := config.Load(config.Options{UserFile: path})
	if err != nil {
		t.Fatal(err)
	}
	if w := keybindingWarnings(reload); len(w) != 1 || !strings.Contains(w[0], "no_such_action") {
		t.Errorf("warnings after the save = %q, want the unknown action", w)
	}
	if got := newKeybindingStore(path, reload).Custom(); len(got) != 1 || got[0].Action != "save" {
		t.Errorf("Custom() after the save = %+v, want the one valid binding", got)
	}
}

// TestSaveKeepsTheShapeOfTheKeybindingsValue: a file in the object form keeps it, and a file
// with a list, or none, gets the list form that Fresh documents.
func TestSaveKeepsTheShapeOfTheKeybindingsValue(t *testing.T) {
	tests := []struct {
		name, file string
		want       string // a fragment the saved keybindings value starts with
	}{
		{"object form stays an object", `{
  // mine
  "keybindings": { "bindings": [ { "key": "u", "modifiers": ["alt"], "action": "to_upper_case", "when": "normal" } ] }
}`, `"keybindings": {`},
		{"list form stays a list", `{"keybindings": [ { "key": "u", "modifiers": ["alt"], "action": "to_upper_case", "when": "normal" } ]}`, `"keybindings": [`},
		{"no keybindings gets the list form", `{"tab_size": 2}`, `"keybindings": [`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := userConfigPath(t, tt.file)
			cfg, _, err := config.Load(config.Options{UserFile: path})
			if err != nil {
				t.Fatal(err)
			}
			store := newKeybindingStore(path, cfg)
			list := append(store.Custom(), ui.CustomBinding{Chord: "ctrl+k", Action: "save", Context: "normal"})
			if _, err := store.Save(list, "default"); err != nil {
				t.Fatal(err)
			}
			text := readTestFile(t, path)
			if !strings.Contains(text, tt.want) {
				t.Errorf("the saved file lacks %q:\n%s", tt.want, text)
			}
			if reload, diags, err := config.Load(config.Options{UserFile: path}); err != nil || len(diags) != 0 || len(newKeybindingStore(path, reload).Custom()) != len(list) {
				t.Errorf("the saved file does not reload to %d bindings: %v %v\n%s", len(list), diags, err, text)
			}
		})
	}
}

// TestPreviewKeepsThePluginBindings: the keybinding editor previews its pending list over the
// plugins' bindings, so a plugin's key still works, and still shows, while the list changes. The
// plain Preview has no plugin layer, which is why the editor asks for this one.
func TestPreviewKeepsThePluginBindings(t *testing.T) {
	path := userConfigPath(t, `{"tab_size": 2}`)
	store := newKeybindingStore(path, config.Config{})
	plugins := []pluginapi.Binding{{Plugin: "jumper", Chord: "ctrl+j", Action: "jumper.jump"}}
	pending := []ui.CustomBinding{{Chord: "ctrl+k", Action: "save", Context: "normal"}}

	withPlugins, err := store.PreviewWithPlugins(pending, "default", plugins)
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := withPlugins.Resolve(ui.KeyPress{Name: "j", Mods: []string{"ctrl"}}, ui.KeyContextNormal); !ok || b.Action != "jumper.jump" {
		t.Errorf("ctrl+j with the plugin layer = %+v, %v; want jumper.jump", b, ok)
	}
	plain, err := store.Preview(pending, "default")
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := plain.Resolve(ui.KeyPress{Name: "j", Mods: []string{"ctrl"}}, ui.KeyContextNormal); ok && b.Action == "jumper.jump" {
		t.Errorf("the plain Preview has the plugin binding %+v", b)
	}
	if b, ok := withPlugins.Resolve(ui.KeyPress{Name: "k", Mods: []string{"ctrl"}}, ui.KeyContextNormal); !ok || b.Action != "save" {
		t.Errorf("ctrl+k in the pending list = %+v, %v; want save", b, ok)
	}
}
