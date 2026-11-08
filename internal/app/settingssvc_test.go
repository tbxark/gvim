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
	if !slices.Contains(change.Live, "editor.tab_size") {
		t.Errorf("Live = %v, want the changed tab size", change.Live)
	}
	if slices.Contains(change.Live, "theme") {
		t.Errorf("Live = %v, want only the settings that changed", change.Live)
	}
	if !change.Reload {
		t.Error("change.Reload is false, want a Reload change")
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

// TestSettingsReloadNoChanges checks the no-change reload: nothing to apply, and
// the change says it was a reload.
func TestSettingsReloadNoChanges(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	userFile := filepath.Join(dir, "config.json")
	writeTestFile(t, userFile, "{}")
	got, err := buildCaptured(t, Options{Root: dir, Config: userFile})
	apptest.Must(t, err)
	change, err := settingsPort(t, got).Reload()
	apptest.Must(t, err)
	if len(change.Live) != 0 || len(change.Restart) != 0 {
		t.Fatalf("Live = %v, Restart = %v, want no changes", change.Live, change.Restart)
	}
	if !change.Reload {
		t.Error("change.Reload is false")
	}
}

// TestSettingsStructureFormatters checks the formatters map sub-page: its rows
// are the layer's entries with their config paths.
func TestSettingsStructureFormatters(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	userFile := filepath.Join(dir, "config.json")
	writeTestFile(t, userFile, "{\n  \"formatters\": {\"go\": \"gofmt\", \".py\": \"black\"}\n}\n")
	got, err := buildCaptured(t, Options{Root: dir, Config: userFile})
	apptest.Must(t, err)
	st, err := settingsPort(t, got).Structure("formatters", tui.SettingsLayerUser)
	apptest.Must(t, err)
	if st.Kind != "map" || st.Title != "Formatters" {
		t.Fatalf("structure = %+v, want the formatters map", st)
	}
	if len(st.Rows) != 2 {
		t.Fatalf("rows = %+v, want two entries", st.Rows)
	}
	// The rows are sorted by key, and "." sorts before "g".
	pyRow, goRow := st.Rows[0], st.Rows[1]
	if !slices.Equal(pyRow.Path, []string{"formatters", ".py"}) || pyRow.Value != "black" {
		t.Errorf("first row = %+v, want formatters..py = black", pyRow)
	}
	if !slices.Equal(goRow.Path, []string{"formatters", "go"}) || goRow.Value != "gofmt" {
		t.Errorf("second row = %+v, want formatters.go = gofmt", goRow)
	}
}

// TestSettingsStructureLanguagesAndFields checks the language list and one
// language's scalar override page.
func TestSettingsStructureLanguagesAndFields(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	userFile := filepath.Join(dir, "config.json")
	writeTestFile(t, userFile, "{\n  \"languages\": {\n    \"go\": {\"tab_size\": 2, \"use_tabs\": true},\n    \"python\": {}\n  }\n}\n")
	got, err := buildCaptured(t, Options{Root: dir, Config: userFile})
	apptest.Must(t, err)
	port := settingsPort(t, got)

	list, err := port.Structure("languages", tui.SettingsLayerUser)
	apptest.Must(t, err)
	if list.Kind != "languages" || len(list.Rows) != 2 {
		t.Fatalf("languages = %+v, want the two entries", list)
	}
	if list.Rows[0].Name != "go" || list.Rows[0].Child != "languages.go" {
		t.Errorf("first row = %+v, want go with its child page", list.Rows[0])
	}
	if !slices.Contains(list.Available, "markdown") || slices.Contains(list.Available, "go") {
		t.Errorf("available = %v, want the known languages not configured", list.Available)
	}

	fields, err := port.Structure("languages.go", tui.SettingsLayerUser)
	apptest.Must(t, err)
	if fields.Kind != "fields" {
		t.Fatalf("kind = %q, want fields", fields.Kind)
	}
	byName := make(map[string]tui.SettingsStructureRow, len(fields.Rows))
	for _, row := range fields.Rows {
		byName[row.Name] = row
	}
	if row := byName["tab_size"]; row.Value != "2" || row.Kind != "int" || !slices.Equal(row.Path, []string{"languages", "go", "tab_size"}) {
		t.Errorf("tab_size row = %+v, want 2", row)
	}
	if row := byName["use_tabs"]; row.Value != "on" || row.Kind != "bool" {
		t.Errorf("use_tabs row = %+v, want on", row)
	}
	if row := byName["auto_indent"]; row.Value != "" {
		t.Errorf("auto_indent row = %+v, want the unset override", row)
	}
}

// TestSettingsStructureStatusBar checks the dual list: sides, available elements
// and the brace spelling.
func TestSettingsStructureStatusBar(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	userFile := filepath.Join(dir, "config.json")
	writeTestFile(t, userFile, "{\n  \"editor\": {\"status_bar\": {\"left\": [\"{filename}\"], \"right\": [\"{trust}\"]}}\n}\n")
	got, err := buildCaptured(t, Options{Root: dir, Config: userFile})
	apptest.Must(t, err)
	st, err := settingsPort(t, got).Structure("editor.status_bar.right", tui.SettingsLayerUser)
	apptest.Must(t, err)
	if st.Kind != "dual" || st.Key != "editor.status_bar.right" {
		t.Fatalf("structure = %+v, want the right dual list", st)
	}
	var left, right []string
	for _, row := range st.Rows {
		switch row.Group {
		case "left":
			left = append(left, row.Name)
		case "right":
			right = append(right, row.Name)
		}
	}
	if !slices.Equal(left, []string{"{filename}"}) || !slices.Equal(right, []string{"{trust}"}) {
		t.Errorf("left = %v right = %v, want the layer's lists", left, right)
	}
	if !slices.Contains(st.Available, "{clock}") || slices.Contains(st.Available, "{filename}") {
		t.Errorf("available = %v, want the elements not on a side", st.Available)
	}
}

// TestSettingsStructureShellArgs checks the terminal.shell.args list page.
func TestSettingsStructureShellArgs(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	userFile := filepath.Join(dir, "config.json")
	writeTestFile(t, userFile, "{\"terminal\": {\"shell\": {\"command\": \"/bin/zsh\", \"args\": [\"-l\", \"-i\"]}}}\n")
	got, err := buildCaptured(t, Options{Root: dir, Config: userFile})
	apptest.Must(t, err)
	st, err := settingsPort(t, got).Structure("terminal.shell.args", tui.SettingsLayerUser)
	apptest.Must(t, err)
	if st.Kind != "list" || len(st.Rows) != 2 {
		t.Fatalf("structure = %+v, want the two arguments", st)
	}
	if st.Rows[0].Name != "-l" || !slices.Equal(st.Rows[0].Path, []string{"terminal", "shell", "args"}) {
		t.Errorf("first row = %+v, want -l", st.Rows[0])
	}
}

// TestSettingsSaveStructuredEdits patches map, language and list entries in one
// save and keeps the rest of the file.
func TestSettingsSaveStructuredEdits(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	userFile := filepath.Join(dir, "config.json")
	writeTestFile(t, userFile, `// keep
{
  "theme": "dark",
  "formatters": { "go": "gofmt" },
  "languages": { "go": { "tab_size": 8 } },
  "editor": { "status_bar": { "left": ["{filename}"], "separator": "|" } }
}
`)
	got, err := buildCaptured(t, Options{Root: dir, Config: userFile})
	apptest.Must(t, err)
	port := settingsPort(t, got)

	change, err := port.Save([]tui.SettingEdit{
		{Path: []string{"formatters", ".py"}, Raw: []byte(`"black"`)},
		{Path: []string{"languages", "go", "tab_size"}, Remove: true},
		{Path: []string{"terminal", "shell", "args"}, Raw: []byte(`["-l"]`)},
	}, tui.SettingsLayerUser)
	apptest.Must(t, err)
	if len(change.Restart) == 0 || !slices.Contains(change.Restart, "terminal.shell.args") {
		t.Errorf("Restart = %v, want terminal.shell.args (read at startup)", change.Restart)
	}
	if !slices.Contains(change.Live, "formatters") || !slices.Contains(change.Live, "languages") {
		t.Errorf("Live = %v, want formatters and languages", change.Live)
	}
	data, err := os.ReadFile(userFile)
	apptest.Must(t, err)
	file := string(data)
	for _, want := range []string{"// keep", `".py": "black"`, `"separator": "|"`} {
		if !strings.Contains(file, want) {
			t.Errorf("patched file misses %q:\n%s", want, file)
		}
	}
	if strings.Contains(file, `"tab_size": 8`) {
		t.Errorf("the reset language override is still there:\n%s", file)
	}
	cfg, _, err := config.Load(config.Options{UserFile: userFile})
	apptest.Must(t, err)
	if cfg.Formatters[".py"] != "black" {
		t.Errorf("formatters = %v, want the added python entry", cfg.Formatters)
	}
	if entry := cfg.Languages["go"]; entry.TabSize != nil {
		t.Errorf("languages.go.tab_size = %v, want the reset override", *entry.TabSize)
	}
	if cfg.StatusBar.Separator != "|" {
		t.Errorf("separator = %q, want the untouched |", cfg.StatusBar.Separator)
	}
	if !slices.Equal(cfg.Terminal.Shell.Args, []string{"-l"}) {
		t.Errorf("shell args = %v, want the saved -l", cfg.Terminal.Shell.Args)
	}
}

// TestSettingsSaveDualListKeepsTheSeparator checks the condition that the dual
// list patches only the left/right paths.
func TestSettingsSaveDualListKeepsTheSeparator(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	userFile := filepath.Join(dir, "config.json")
	writeTestFile(t, userFile, `{
  "editor": { "status_bar": { "left": ["{filename}"], "right": ["{trust}"], "separator": "|" } }
}
`)
	got, err := buildCaptured(t, Options{Root: dir, Config: userFile})
	apptest.Must(t, err)
	port := settingsPort(t, got)
	_, err = port.Save([]tui.SettingEdit{
		{Path: []string{"editor", "status_bar", "left"}, Raw: []byte(`["{filename}", "{clock}"]`)},
		{Path: []string{"editor", "status_bar", "right"}, Raw: []byte(`["{trust}"]`)},
	}, tui.SettingsLayerUser)
	apptest.Must(t, err)
	data, err := os.ReadFile(userFile)
	apptest.Must(t, err)
	if !strings.Contains(string(data), `"separator": "|"`) {
		t.Fatalf("the separator was changed:\n%s", data)
	}
	cfg, _, err := config.Load(config.Options{UserFile: userFile})
	apptest.Must(t, err)
	if !slices.Equal(cfg.StatusBar.Left, []string{"{filename}", "{clock}"}) {
		t.Errorf("left = %v, want the saved list", cfg.StatusBar.Left)
	}
}

// TestSettingsSaveRejectsUnknownStructuredWrites checks the write guard.
func TestSettingsSaveRejectsUnknownStructuredWrites(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	userFile := filepath.Join(dir, "config.json")
	writeTestFile(t, userFile, "{}")
	got, err := buildCaptured(t, Options{Root: dir, Config: userFile})
	apptest.Must(t, err)
	port := settingsPort(t, got)
	if _, err := port.Save([]tui.SettingEdit{{Path: []string{"theme", "nested"}, Raw: []byte("1")}}, tui.SettingsLayerUser); err == nil {
		t.Fatal("Save accepted a path the page cannot write")
	}
	if _, err := port.Save([]tui.SettingEdit{{Path: []string{"formatters", "go"}, Raw: []byte(`""`)}}, tui.SettingsLayerUser); err == nil || !strings.Contains(err.Error(), "must not be empty") {
		t.Fatalf("Save with an empty formatter command = %v, want the validation error", err)
	}
}

// TestSettingsStructureProjectNeedsTrust checks the trust gate for structures.
func TestSettingsStructureProjectNeedsTrust(t *testing.T) {
	isolateConfig(t)
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "go.mod"), "module x\n")
	got, err := buildCaptured(t, Options{Root: root})
	apptest.Must(t, err)
	if _, err := settingsPort(t, got).Structure("formatters", tui.SettingsLayerProject); err == nil || !strings.Contains(err.Error(), "trusted workspace") {
		t.Fatalf("Structure for the project layer = %v, want the trust refusal", err)
	}
}
