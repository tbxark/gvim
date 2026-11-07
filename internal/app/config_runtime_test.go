package app

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/config"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/explorer"
	"github.com/tbxark/gvim/internal/keymap"
	"github.com/tbxark/gvim/internal/ui"
)

// writeTestFile writes content to path, creating the parent folders.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// projectRoot returns options that open a new project folder with one file.
// When body is not empty it is written as the project config file.
func projectRoot(t *testing.T, body string) Options {
	t.Helper()
	root := t.TempDir()
	file := filepath.Join(root, "a.txt")
	writeTestFile(t, file, "hello\n")
	if body != "" {
		writeTestFile(t, filepath.Join(root, ".gvim", "config.json"), body)
	}
	return Options{Root: root, Files: []FileTarget{{Path: file}}}
}

// buildCaptured runs buildModel with the editor and UI constructors replaced by
// recorders. The test sees the values the config produced, and the model is
// built for real. The user config file is isolated.
func buildCaptured(t *testing.T, opts Options) (built, error) {
	t.Helper()
	isolateConfig(t)
	return buildRecorded(t, opts)
}

// buildRecorded is buildCaptured without the isolation. A test that writes its
// own user config file first calls it after isolateConfig.
func buildRecorded(t *testing.T, opts Options) (built, error) {
	t.Helper()
	var got built
	prevEditor, prevUI := newEditor, newUI
	t.Cleanup(func() { newEditor, newUI = prevEditor, prevUI })
	newEditor = func(o editor.Options) *editor.Editor {
		// The workspace root is checked by TestEditorRootIsTheWorkspaceRoot, so the
		// settings compared here leave it out.
		got.root = o.Root
		settings := o
		settings.Root = ""
		got.editor = settings
		return prevEditor(o)
	}
	newUI = func(d ui.Deps) *ui.Model {
		got.deps = d
		return prevUI(d)
	}
	_, files, err := buildModel(t.Context(), opts)
	if err != nil {
		return built{}, err
	}
	t.Cleanup(files.Close)
	return got, nil
}

// built is what buildModel passes to the editor and UI constructors.
type built struct {
	editor editor.Options
	root   string // the workspace root the editor was given
	deps   ui.Deps
}

// keymapOf returns the keymap that the UI resolves keys with.
func keymapOf(t *testing.T, d ui.Deps) *keymap.Map {
	t.Helper()
	a, ok := d.Keys.(keymapAdapter)
	if !ok {
		t.Fatalf("Keys is %T, want keymapAdapter", d.Keys)
	}
	return a.km
}

// resolveAction returns the action that chord runs in the normal context.
func resolveAction(t *testing.T, km *keymap.Map, chord string) string {
	t.Helper()
	b, ok := km.Resolve(chord, keymap.ContextNormal)
	if !ok {
		t.Fatalf("%s is unbound in normal", chord)
	}
	return b.Action
}

// defaultEditorOptions is what buildModel passes to editor.New with no config
// file. The project has a file, so Empty is false.
var defaultEditorOptions = editor.Options{
	TabSize:           4,
	AutoIndent:        true,
	DefaultLineEnding: "LF",

	// The Fresh editor.* defaults, which the config supplies without a file.
	AutoClose:                              true,
	AutoSurround:                           true,
	VirtualSpace:                           "off",
	ScrollOffset:                           3,
	AutoReadOnly:                           true,
	SnapshotInterval:                       100,
	AutoCreateEmptyBufferOnLastBufferClose: true,
	RecoveryEnabled:                        true,
	AutoRecoverySaveIntervalSecs:           2,
	AutoRevertPollIntervalMS:               2000,
	LargeFileThresholdBytes:                10485760,
	LargeFileMode:                          true,
	EstimatedLineLength:                    80,
}

// withEditorDefaults is defaultEditorOptions with the tab size and line ending replaced.
func withEditorDefaults(tabSize int, ending string) editor.Options {
	o := defaultEditorOptions
	o.TabSize = tabSize
	o.DefaultLineEnding = ending
	return o
}

func TestConfigEditorSettingsReachEditor(t *testing.T) {
	with := func(f func(*editor.Options)) editor.Options {
		o := defaultEditorOptions
		f(&o)
		return o
	}
	tests := []struct {
		name string
		body string
		want editor.Options
	}{
		{"no config file", ``, defaultEditorOptions},
		{"tab_size", `{"tab_size": 2}`, with(func(o *editor.Options) { o.TabSize = 2 })},
		{"insert_spaces false", `{"insert_spaces": false}`, with(func(o *editor.Options) { o.UseTabs = true })},
		{"auto_indent false", `{"auto_indent": false}`, with(func(o *editor.Options) { o.AutoIndent = false })},
		{"trim_trailing_whitespace", `{"trim_trailing_whitespace": true}`, with(func(o *editor.Options) { o.TrimTrailingWhitespace = true })},
		{"ensure_final_newline", `{"ensure_final_newline": true}`, with(func(o *editor.Options) { o.EnsureFinalNewline = true })},
		{"line_ending crlf", `{"line_ending": "crlf"}`, with(func(o *editor.Options) { o.DefaultLineEnding = "CRLF" })},
		{"line_ending auto", `{"line_ending": "auto"}`, defaultEditorOptions},
		{"line_ending lf", `{"line_ending": "lf"}`, defaultEditorOptions},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildCaptured(t, projectRoot(t, tt.body))
			if err != nil {
				t.Fatalf("buildModel() error = %v", err)
			}
			if got.editor != tt.want {
				t.Fatalf("editor.Options = %+v\nwant %+v", got.editor, tt.want)
			}
		})
	}
}

func TestConfigViewSettingsReachUI(t *testing.T) {
	tests := []struct {
		name            string
		body            string
		hideLineNumbers bool
		hideMenuBar     bool
	}{
		{"defaults", ``, false, false},
		{"line_numbers false", `{"line_numbers": false}`, true, false},
		{"show_menu_bar false", `{"show_menu_bar": false}`, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildCaptured(t, projectRoot(t, tt.body))
			if err != nil {
				t.Fatalf("buildModel() error = %v", err)
			}
			if got.deps.HideLineNumbers != tt.hideLineNumbers {
				t.Errorf("HideLineNumbers = %v, want %v", got.deps.HideLineNumbers, tt.hideLineNumbers)
			}
			if got.deps.HideMenuBar != tt.hideMenuBar {
				t.Errorf("HideMenuBar = %v, want %v", got.deps.HideMenuBar, tt.hideMenuBar)
			}
		})
	}
}

func TestConfigThemeIsUsedWithoutFlag(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		themeFlag string
		want      string
	}{
		{"default theme", ``, "", "dark"},
		{"config theme", `{"theme": "nord"}`, "", "nord"},
		{"flag beats config", `{"theme": "nord"}`, "light", "light"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := projectRoot(t, tt.body)
			opts.Theme = tt.themeFlag
			got, err := buildCaptured(t, opts)
			if err != nil {
				t.Fatalf("buildModel() error = %v", err)
			}
			if got.deps.Theme.Name != tt.want {
				t.Fatalf("theme = %q, want %q", got.deps.Theme.Name, tt.want)
			}
		})
	}
}

func TestConfigKeymapIsUsedWithoutFlag(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		keymapFlag string
		want       string
	}{
		{"platform default", ``, "", keymap.DefaultName()},
		{"config keymap", `{"keymap": "emacs"}`, "", "emacs"},
		{"flag beats config", `{"keymap": "emacs"}`, "vscode", "vscode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := projectRoot(t, tt.body)
			opts.Keymap = tt.keymapFlag
			got, err := buildCaptured(t, opts)
			if err != nil {
				t.Fatalf("buildModel() error = %v", err)
			}
			want, err := keymap.ByName(tt.want)
			if err != nil {
				t.Fatal(err)
			}
			if keymapOf(t, got.deps) != want {
				t.Fatalf("keymap is not the %q preset", tt.want)
			}
		})
	}
}

func TestConfigKeybindingsOverridePreset(t *testing.T) {
	body := `{"keymap": "emacs", "keybindings": {"bindings": [
		{"key": "s", "modifiers": ["ctrl"], "action": "noop", "when": "normal"}]}}`
	got, err := buildCaptured(t, projectRoot(t, body))
	if err != nil {
		t.Fatalf("buildModel() error = %v", err)
	}
	if action := resolveAction(t, keymapOf(t, got.deps), "ctrl+s"); action != "noop" {
		t.Fatalf("ctrl+s = %q, want noop from the keybindings override", action)
	}
	plain, err := buildCaptured(t, projectRoot(t, `{"keymap": "emacs"}`))
	if err != nil {
		t.Fatal(err)
	}
	if action := resolveAction(t, keymapOf(t, plain.deps), "ctrl+s"); action == "noop" {
		t.Fatal("ctrl+s is noop without any keybindings override")
	}
}

// TestBundledDocKeybindingsExampleLoads checks that the keybindings example of
// docs/plugins/bundled.md is valid config and binds the plugin command, and that the
// map-of-chords form it once showed is refused.
func TestBundledDocKeybindingsExampleLoads(t *testing.T) {
	doc, err := os.ReadFile("../../docs/plugins/bundled.md")
	if err != nil {
		t.Fatal(err)
	}
	_, rest, found := strings.Cut(string(doc), "```json\n")
	if !found {
		t.Fatal("bundled.md has no json example")
	}
	example, _, _ := strings.Cut(rest, "```")
	if !strings.Contains(example, `"keybindings"`) {
		t.Fatalf("the first json example is not the keybindings one: %s", example)
	}
	got, err := buildCaptured(t, projectRoot(t, example))
	if err != nil {
		t.Fatalf("the documented keybindings example does not load: %v", err)
	}
	if action := resolveAction(t, keymapOf(t, got.deps), "ctrl+alt+j"); action != "flash.jump" {
		t.Fatalf("ctrl+alt+j = %q, want flash.jump", action)
	}
	if _, err := buildCaptured(t, projectRoot(t, `{"keybindings": {"ctrl+alt+j": "flash.jump"}}`)); err == nil {
		t.Fatal("a map of chords is accepted as keybindings")
	}
}

// A global noop without "when" unbinds ctrl+s everywhere. The default keymap
// binds save to ctrl+s in normal context, so that binding must not survive.
func TestConfigGlobalNoopUnbindsSaveInNormal(t *testing.T) {
	body := `{"keymap": "default", "keybindings": {"bindings": [
		{"key": "s", "modifiers": ["ctrl"], "action": "noop"}]}}`
	got, err := buildCaptured(t, projectRoot(t, body))
	if err != nil {
		t.Fatalf("buildModel() error = %v", err)
	}
	if action := resolveAction(t, keymapOf(t, got.deps), "ctrl+s"); action != "noop" {
		t.Fatalf("ctrl+s in normal = %q, want noop from the global keybindings override", action)
	}
}

func TestConfigExplorerSettingsReachTree(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantHidden  bool // .env is listed
		wantIgnored bool // build.log is listed
	}{
		{"defaults", ``, false, false},
		{"explorer_show_hidden", `{"explorer_show_hidden": true}`, true, false},
		{"explorer_show_ignored", `{"explorer_show_ignored": true}`, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := projectRoot(t, tt.body)
			writeTestFile(t, filepath.Join(opts.Root, ".env"), "SECRET=1\n")
			writeTestFile(t, filepath.Join(opts.Root, "build.log"), "log\n")
			writeTestFile(t, filepath.Join(opts.Root, ".gitignore"), "*.log\n")
			got, err := buildCaptured(t, opts)
			if err != nil {
				t.Fatalf("buildModel() error = %v", err)
			}
			tree, ok := got.deps.Explorer.(*explorer.Tree)
			if !ok {
				t.Fatalf("Explorer is %T, want *explorer.Tree", got.deps.Explorer)
			}
			var names []string
			for _, r := range tree.Rows() {
				names = append(names, r.Name)
			}
			if slices.Contains(names, ".env") != tt.wantHidden {
				t.Errorf(".env listed = %v, want %v; rows %v", !tt.wantHidden, tt.wantHidden, names)
			}
			if slices.Contains(names, "build.log") != tt.wantIgnored {
				t.Errorf("build.log listed = %v, want %v; rows %v", !tt.wantIgnored, tt.wantIgnored, names)
			}
		})
	}
}

func TestBuildModelRejectsBrokenConfigOverrides(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"keybindings unknown key", `{"keybindings": {"bindings": [{"key": "Weird", "modifiers": [], "action": "save"}]}}`, `unknown key name "Weird"`},
		{"keybindings misspelled field", `{"keybindings": {"binding": []}}`, "unknown field"},
		{"keymap unknown", `{"keymap": "vim"}`, `unknown keymap "vim"`},
		{"theme unknown", `{"theme": "neon"}`, `unknown theme "neon"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := projectRoot(t, tt.body)
			_, err := buildCaptured(t, opts)
			if err == nil {
				t.Fatal("buildModel() returned no error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.want)
			}
			projFile := filepath.Join(opts.Root, ".gvim", "config.json")
			if !strings.Contains(err.Error(), projFile) {
				t.Fatalf("error = %v, want it to name %s", err, projFile)
			}
		})
	}
}

func TestPrintDiagnosticsWritesEachWarning(t *testing.T) {
	var buf bytes.Buffer
	printDiagnostics(&buf, []config.Diagnostic{
		{Path: "/x/config.json", Key: "tab_size", Message: "must be between 1 and 16, got 99"},
		{Message: "no home directory"},
	})
	want := "gvim: warning: /x/config.json: tab_size: must be between 1 and 16, got 99\n" +
		"gvim: warning: no home directory\n"
	if got := buf.String(); got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func TestBuildModelPrintsConfigDiagnosticsToStderr(t *testing.T) {
	opts := projectRoot(t, `{"tab_size": 99}`)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = w
	_, buildErr := buildCaptured(t, opts)
	os.Stderr = saved
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if _, err := stderr.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	if buildErr != nil {
		t.Fatalf("buildModel() error = %v", buildErr)
	}
	if !strings.Contains(stderr.String(), "gvim: warning: ") || !strings.Contains(stderr.String(), "tab_size: must be between 1 and 16") {
		t.Fatalf("stderr = %q, want the tab_size warning", stderr.String())
	}
}

func TestConfigFlagFileIsReadWithoutProjectOrUserFile(t *testing.T) {
	opts := projectRoot(t, "")
	alt := filepath.Join(t.TempDir(), "alt.json")
	writeTestFile(t, alt, `{"tab_size": 2}`)
	opts.Config = alt
	got, err := buildCaptured(t, opts)
	if err != nil {
		t.Fatalf("buildModel() error = %v", err)
	}
	want := defaultEditorOptions
	want.TabSize = 2
	if got.editor != want {
		t.Fatalf("editor.Options = %+v\nwant %+v", got.editor, want)
	}
}

// A --config path that does not exist stops startup and names the path. The
// default user file must not quietly take its place.
func TestBuildModelRejectsMissingConfigFlagFile(t *testing.T) {
	isolateConfig(t)
	files, err := config.Paths()
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, files.User, `{"tab_size": 3}`)
	opts := projectRoot(t, ``)
	missing := filepath.Join(t.TempDir(), "absent.json")
	opts.Config = missing
	_, err = buildRecorded(t, opts)
	if err == nil {
		t.Fatal("buildModel() returned no error for a missing --config file")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Fatalf("error = %v, want it to name %s", err, missing)
	}
}

func TestConfigFlagReplacesUserFileAndProjectStillApplies(t *testing.T) {
	isolateConfig(t)
	files, err := config.Paths()
	if err != nil {
		t.Fatal(err)
	}
	// The default user file would set tab_size 3 and auto_indent false. The
	// flag file replaces it, so neither value may reach the editor.
	writeTestFile(t, files.User, `{"tab_size": 3, "auto_indent": false}`)
	alt := filepath.Join(t.TempDir(), "alt.json")
	writeTestFile(t, alt, `{"line_ending": "crlf"}`)
	tests := []struct {
		name    string
		project string
		want    editor.Options
	}{
		{"no project file", ``, withEditorDefaults(4, "CRLF")},
		{"project file applies over the flag file", `{"tab_size": 2}`, withEditorDefaults(2, "CRLF")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := projectRoot(t, tt.project)
			opts.Config = alt
			got, err := buildRecorded(t, opts)
			if err != nil {
				t.Fatalf("buildModel() error = %v", err)
			}
			if got.editor != tt.want {
				t.Fatalf("editor.Options = %+v\nwant %+v", got.editor, tt.want)
			}
		})
	}
}

func TestBuildModelNoticesFirstConfigDiagnostic(t *testing.T) {
	opts := projectRoot(t, `{"tab_size": 99}`)
	got, err := buildCaptured(t, opts)
	if err != nil {
		t.Fatalf("buildModel() error = %v", err)
	}
	// resolveRoot makes the root canonical, so the file is named by its real path.
	root, err := filepath.EvalSymlinks(opts.Root)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, ".gvim", "config.json") + ": tab_size: must be between 1 and 16, got 99"
	if got.deps.Notice != want {
		t.Fatalf("Notice = %q, want %q", got.deps.Notice, want)
	}
	clean, err := buildCaptured(t, projectRoot(t, `{"tab_size": 2}`))
	if err != nil {
		t.Fatalf("buildModel() error = %v", err)
	}
	if clean.deps.Notice != "" {
		t.Fatalf("Notice = %q with no config problem, want empty", clean.deps.Notice)
	}
}

// The editor measures copy_relative_file_path from the resolved workspace root.
func TestEditorRootIsTheWorkspaceRoot(t *testing.T) {
	opts := projectRoot(t, "")
	got, err := buildCaptured(t, opts)
	if err != nil {
		t.Fatalf("buildModel() error = %v", err)
	}
	want, err := filepath.EvalSymlinks(opts.Root)
	if err != nil {
		t.Fatal(err)
	}
	if got.root != want {
		t.Errorf("editor root = %q, want the workspace root %q", got.root, want)
	}
}
