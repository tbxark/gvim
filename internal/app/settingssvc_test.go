package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/trust"
)

// settingsPort returns the SettingsPort of a built runtime.
func settingsPort(t *testing.T, got built) tui.SettingsPort {
	t.Helper()
	if got.deps.SettingsPort == nil {
		t.Fatal("Deps.SettingsPort is nil")
	}
	return got.deps.SettingsPort
}

// findItem returns the snapshot item with the dotted key.
func findItem(t *testing.T, snap tui.SettingsSnapshot, key string) tui.SettingsItem {
	t.Helper()
	for _, sec := range snap.Sections {
		for _, it := range sec.Items {
			if it.Key == key {
				return it
			}
		}
	}
	t.Fatalf("setting %q is not in the snapshot", key)
	return tui.SettingsItem{}
}

// TestSettingsSnapshot lists the schema through the service: every setting is
// there, grouped in sections, with the theme and keymap choices the app knows.
func TestSettingsSnapshot(t *testing.T) {
	isolateConfig(t)
	got, err := buildCaptured(t, Options{Root: t.TempDir()})
	apptest.Must(t, err)
	snap := settingsPort(t, got).Snapshot()

	var items int
	for _, sec := range snap.Sections {
		if sec.Name == "" || len(sec.Items) == 0 {
			t.Errorf("section %+v is empty", sec)
		}
		items += len(sec.Items)
	}
	if want := len(config.Settings()); items != want {
		t.Fatalf("snapshot has %d settings, want %d", items, want)
	}
	if snap.UserFile == "" {
		t.Error("the user config file is unknown")
	}
	themeItem := findItem(t, snap, "theme")
	if len(themeItem.Choices) == 0 || !slices.Contains(themeItem.Choices, "dark") {
		t.Errorf("theme choices = %v, want the bundled themes", themeItem.Choices)
	}
	keymapItem := findItem(t, snap, "keymap")
	if len(keymapItem.Choices) == 0 || !slices.Contains(keymapItem.Choices, "emacs") {
		t.Errorf("keymap choices = %v, want the built-in keymaps", keymapItem.Choices)
	}
	if got, want := findItem(t, snap, "editor.tab_size").Value, "4"; got != want {
		t.Errorf("tab_size = %q, want %q", got, want)
	}
	if !snap.ProjectWritable {
		t.Error("a plain (silently trusted) workspace does not offer the project layer")
	}
}

// TestSettingsProjectLayerNeedsTrust checks the trust gate: an undecided
// executable workspace does not offer the project layer and refuses to write it.
func TestSettingsProjectLayerNeedsTrust(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module x\n")
	got, err := buildCaptured(t, Options{Root: root})
	apptest.Must(t, err)
	port := settingsPort(t, got)
	if snap := port.Snapshot(); snap.ProjectWritable {
		t.Error("an untrusted workspace offers the project layer")
	}
	if _, err := port.Save([]tui.SettingEdit{{Key: "editor.tab_size", Value: "2"}}, tui.SettingsLayerProject); err == nil || !strings.Contains(err.Error(), "trusted workspace") {
		t.Fatalf("Save to the project layer = %v, want the trusted-workspace refusal", err)
	}
}

// TestSettingsSaveUserLayer patches the user file with comments preserved and
// applies the live editor value.
func TestSettingsSaveUserLayer(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	userFile := filepath.Join(dir, "config.json")
	writeTestFile(t, userFile, "// my settings\n{\n  \"theme\": \"dark\"\n}\n")
	file := filepath.Join(dir, "a.go")
	writeTestFile(t, file, "package main\n")
	got, err := buildCaptured(t, Options{Root: dir, Config: userFile, Files: []FileTarget{{Path: file}}})
	apptest.Must(t, err)
	port := settingsPort(t, got)

	change, err := port.Save([]tui.SettingEdit{{Key: "editor.tab_size", Value: "8"}}, tui.SettingsLayerUser)
	apptest.Must(t, err)
	if !slices.Contains(change.Live, "editor.tab_size") {
		t.Errorf("Live = %v, want editor.tab_size applied live", change.Live)
	}
	if slices.Contains(change.Restart, "editor.tab_size") {
		t.Errorf("Restart = %v, want editor.tab_size not a restart setting", change.Restart)
	}
	data, err := os.ReadFile(userFile)
	apptest.Must(t, err)
	if !strings.Contains(string(data), "// my settings") || !strings.Contains(string(data), "\"tab_size\": 8") {
		t.Fatalf("the patched user config = %q, want the comment kept and tab_size written", data)
	}
	if v := findItem(t, change.Snapshot, "editor.tab_size").Value; v != "8" {
		t.Errorf("snapshot tab_size = %q, want 8", v)
	}
	// The root pushes the editor-side values on the update loop, through Apply.
	port.Apply(change)
	if s := got.ed.SettingsFor(got.ed.Active()); s.TabSize != 8 {
		t.Errorf("editor SettingsFor tab size = %d, want the live 8", s.TabSize)
	}
}

// TestSettingsSaveRestartSetting checks the restart boundary: a value read only
// while the editor starts is reported as a restart, not applied live.
func TestSettingsSaveRestartSetting(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	got, err := buildCaptured(t, Options{Root: dir})
	apptest.Must(t, err)
	port := settingsPort(t, got)
	change, err := port.Save([]tui.SettingEdit{{Key: "editor.hot_exit", Value: "off"}}, tui.SettingsLayerUser)
	apptest.Must(t, err)
	if !slices.Contains(change.Restart, "editor.hot_exit") {
		t.Errorf("Restart = %v, want editor.hot_exit", change.Restart)
	}
	if slices.Contains(change.Live, "editor.hot_exit") {
		t.Errorf("Live = %v, want editor.hot_exit not live", change.Live)
	}
}

// TestSettingsSaveProjectLayerTrusted writes the project config of a trusted
// workspace and reloads it into the effective config.
func TestSettingsSaveProjectLayerTrusted(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module x\n")
	store := useTrustStore(t)
	apptest.Must(t, store.Set(root, trust.Trusted))
	got, err := buildCaptured(t, Options{Root: root})
	apptest.Must(t, err)
	port := settingsPort(t, got)
	snap := port.Snapshot()
	if !snap.ProjectWritable {
		t.Fatal("a trusted workspace does not offer the project layer")
	}
	canonical, err := trust.CanonicalRoot(root)
	apptest.Must(t, err)
	want := filepath.Join(canonical, ".gvim", "config.json")
	if snap.ProjectFile != want {
		t.Errorf("ProjectFile = %q, want %q", snap.ProjectFile, want)
	}

	change, err := port.Save([]tui.SettingEdit{{Key: "editor.tab_size", Value: "2"}}, tui.SettingsLayerProject)
	apptest.Must(t, err)
	if change.Layer != tui.SettingsLayerProject {
		t.Errorf("change layer = %q, want project", change.Layer)
	}
	if v := findItem(t, change.Snapshot, "editor.tab_size").Value; v != "2" {
		t.Errorf("snapshot tab_size = %q, want 2", v)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("the project config was not written: %v", err)
	}
	port.Apply(change)
	if s := got.ed.SettingsFor(got.ed.Active()); s.TabSize != 2 {
		t.Errorf("editor SettingsFor tab size = %d, want the project 2", s.TabSize)
	}
}

// TestSettingsSaveRefusesABadValue checks that a rejected value leaves both
// files alone.
func TestSettingsSaveRefusesABadValue(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	userFile := filepath.Join(dir, "config.json")
	writeTestFile(t, userFile, "{\"theme\": \"dark\"}\n")
	got, err := buildCaptured(t, Options{Root: dir, Config: userFile})
	apptest.Must(t, err)
	port := settingsPort(t, got)
	before, err := os.ReadFile(userFile)
	apptest.Must(t, err)

	if _, err := port.Save([]tui.SettingEdit{{Key: "editor.tab_size", Value: "0"}}, tui.SettingsLayerUser); err == nil || !strings.Contains(err.Error(), "between 1 and 16") {
		t.Fatalf("Save with tab_size 0 = %v, want the bounds refusal", err)
	}
	after, err := os.ReadFile(userFile)
	apptest.Must(t, err)
	if string(before) != string(after) {
		t.Fatalf("a refused Save changed the file: %q", after)
	}
}

// TestSettingsReloadRereadsTheFiles checks Reload: a value changed on disk is
// loaded again and applied to the running editor.
func TestSettingsReloadRereadsTheFiles(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	userFile := filepath.Join(dir, "config.json")
	writeTestFile(t, userFile, "{}")
	got, err := buildCaptured(t, Options{Root: dir, Config: userFile})
	apptest.Must(t, err)
	port := settingsPort(t, got)
	writeTestFile(t, userFile, "{\"editor\": {\"tab_size\": 6}}")

	change, err := port.Reload()
	apptest.Must(t, err)
	if v := findItem(t, change.Snapshot, "editor.tab_size").Value; v != "6" {
		t.Errorf("snapshot tab_size = %q, want the reloaded 6", v)
	}
	if !slices.Contains(change.Live, "editor.tab_size") || !slices.Contains(change.Live, "theme") {
		t.Errorf("Live = %v, want every live setting after a Reload", change.Live)
	}
	port.Apply(change)
	if s := got.ed.SettingsFor(got.ed.Active()); s.TabSize != 6 {
		t.Errorf("editor SettingsFor tab size = %d, want the reloaded 6", s.TabSize)
	}
}

// TestSettingsFlagWinsOverTheFile checks the command-line rule: the edit reaches
// the file, but the flag keeps the running editor's value and the page is told.
func TestSettingsFlagWinsOverTheFile(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	userFile := filepath.Join(dir, "config.json")
	writeTestFile(t, userFile, "{}")
	got, err := buildCaptured(t, Options{Root: dir, Config: userFile, Theme: "light"})
	apptest.Must(t, err)
	port := settingsPort(t, got)
	if v := findItem(t, port.Snapshot(), "theme").Value; v != "light" {
		t.Errorf("snapshot theme = %q, want the flag's light", v)
	}
	change, err := port.Save([]tui.SettingEdit{{Key: "theme", Value: "dark"}}, tui.SettingsLayerUser)
	apptest.Must(t, err)
	if slices.Contains(change.Live, "theme") {
		t.Errorf("Live = %v, want the theme not applied under --theme", change.Live)
	}
	if !slices.ContainsFunc(change.Warnings, func(w string) bool { return strings.Contains(w, "--theme wins") }) {
		t.Errorf("Warnings = %v, want the --theme notice", change.Warnings)
	}
	data, err := os.ReadFile(userFile)
	apptest.Must(t, err)
	if !strings.Contains(string(data), "\"dark\"") {
		t.Fatalf("the file was not written: %q", data)
	}
}

// TestPatchConfigFileWritesAtomically checks the app-owned write path: the
// patched file keeps its comments, a missing file is created with the edits, and
// an invalid file is left as it was.
func TestPatchConfigFileWritesAtomically(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeTestFile(t, path, "// keep\n{ \"theme\": \"dark\" }\n")
	apptest.Must(t, patchConfigFile(path, []config.Edit{{Path: []string{"editor", "tab_size"}, Raw: []byte("8")}}))
	data, err := os.ReadFile(path)
	apptest.Must(t, err)
	if !strings.Contains(string(data), "// keep") || !strings.Contains(string(data), "\"tab_size\": 8") {
		t.Fatalf("patched file = %q, want the comment and the new value", data)
	}

	created := filepath.Join(dir, "new", "config.json")
	apptest.Must(t, patchConfigFile(created, []config.Edit{{Path: []string{"theme"}, Raw: []byte("\"light\"")}}))
	if _, err := os.Stat(created); err != nil {
		t.Fatalf("the new config file was not created: %v", err)
	}

	const broken = "{\n  \"theme\": \n"
	brokenPath := filepath.Join(dir, "broken.json")
	writeTestFile(t, brokenPath, broken)
	if err := patchConfigFile(brokenPath, []config.Edit{{Path: []string{"editor", "tab_size"}, Raw: []byte("8")}}); err == nil {
		t.Fatal("patchConfigFile accepted an invalid file")
	}
	after, err := os.ReadFile(brokenPath)
	apptest.Must(t, err)
	if string(after) != broken {
		t.Fatalf("the invalid file changed: %q", after)
	}
}
