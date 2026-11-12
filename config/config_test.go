package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// writeFile creates path with content, making parent directories.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// isolateHome points the user config directory at a temp home for the test.
// os.UserConfigDir reads HOME on darwin and XDG_CONFIG_HOME or HOME elsewhere,
// so both are set.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	return home
}

func TestDefaults(t *testing.T) {
	want := Config{
		Theme:        "dark",
		TabSize:      4,
		InsertSpaces: true,
		LineEnding:   "auto",
		AutoIndent:   true,
		LineNumbers:  true,
		ShowMenuBar:  true,

		ShowTabBar:              true,
		ShowStatusBar:           true,
		ShowVerticalScrollbar:   true,
		ShowHorizontalScrollbar: true,
		LineWrap:                true,
		HighlightCurrentLine:    true,
		MouseCapture:            true,
		LSPEnabled:              true,
		CursorStyle:             "block",
		UIIcons:                 "auto",
		UIImageProtocol:         "auto",

		ExplorerShowHidden: true,
		PluginTimeout:      500 * time.Millisecond,

		Terminal:     DefaultTerminalOptions(),
		Daemon:       DefaultDaemonConfig(),
		FileExplorer: DefaultFileExplorer(),
		Env:          DefaultEnvConfig(),
		SSH:          DefaultSSHConfig(),
		Update:       DefaultUpdate(),

		// The Fresh editor.* defaults are asserted option by option in
		// TestEditorDefaultsAreFreshs.
		EditorOptions: defaultEditorOptions(),
	}
	if got := Defaults(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Defaults() = %+v, want %+v", got, want)
	}
}

func TestLoadMissingFilesGivesDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg, diags, err := Load(Options{
		Root:     dir,
		UserFile: filepath.Join(dir, "nope", "config.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("missing files gave diagnostics: %v", diags)
	}
	if cfg.Theme != "dark" || cfg.TabSize != 4 {
		t.Fatalf("cfg = %+v, want defaults", cfg)
	}
}

func TestLoadEmptyRootSkipsProject(t *testing.T) {
	isolateHome(t)
	cfg, diags, err := Load(Options{})
	if err != nil || len(diags) != 0 {
		t.Fatalf("Load = %v, %v", diags, err)
	}
	if cfg.Theme != "dark" {
		t.Fatalf("theme = %q", cfg.Theme)
	}
}

func TestLoadLayering(t *testing.T) {
	dir := t.TempDir()
	userFile := filepath.Join(dir, "user", "config.json")
	root := filepath.Join(dir, "proj")
	projFile := filepath.Join(root, ".gvim", "config.json")
	writeFile(t, userFile, `{"theme": "light", "tab_size": 8, "line_numbers": false}`)
	writeFile(t, projFile, `{"tab_size": 2, "insert_spaces": false}`)

	cfg, diags, err := Load(Options{Root: root, UserFile: userFile})
	if err != nil || len(diags) != 0 {
		t.Fatalf("Load = %v, %v", diags, err)
	}
	// Project overrides user for tab_size, user overrides default for theme,
	// project overrides default for insert_spaces, and an unset key keeps the
	// default.
	if cfg.TabSize != 2 {
		t.Errorf("tab_size = %d, want 2 (project over user)", cfg.TabSize)
	}
	if cfg.Theme != "light" {
		t.Errorf("theme = %q, want light (user over default)", cfg.Theme)
	}
	if cfg.InsertSpaces {
		t.Errorf("insert_spaces = true, want false (project over default)")
	}
	if cfg.LineNumbers {
		t.Errorf("line_numbers = true, want false (user over default)")
	}
	if !cfg.AutoIndent {
		t.Errorf("auto_indent = false, want default true")
	}
}

func TestLoadProjectOnlyLayer(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "proj")
	writeFile(t, filepath.Join(root, ".gvim", "config.json"), `{"line_ending": "crlf"}`)
	cfg, diags, err := Load(Options{Root: root, UserFile: filepath.Join(dir, "absent.json")})
	if err != nil || len(diags) != 0 {
		t.Fatalf("Load = %v, %v", diags, err)
	}
	if cfg.LineEnding != "crlf" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadJSONCCommentsAndTrailingCommas(t *testing.T) {
	dir := t.TempDir()
	userFile := filepath.Join(dir, "config.json")
	writeFile(t, userFile, `// gvim settings
{
  /* indentation */
  "tab_size": 3, // three columns
  "insert_spaces": false,
  "theme": "solarized", /* trailing comma follows */
  "keybindings": {
    "ctrl+s": "save", // comment inside raw value
  },
}
`)
	cfg, diags, err := Load(Options{UserFile: userFile})
	if err != nil || len(diags) != 0 {
		t.Fatalf("Load = %v, %v", diags, err)
	}
	if cfg.TabSize != 3 || cfg.InsertSpaces || cfg.Theme != "solarized" {
		t.Fatalf("cfg = %+v", cfg)
	}
	var kb map[string]string
	if err := json.Unmarshal(cfg.Keybindings, &kb); err != nil {
		t.Fatalf("keybindings %s is not standard JSON: %v", cfg.Keybindings, err)
	}
	if kb["ctrl+s"] != "save" {
		t.Fatalf("keybindings = %v", kb)
	}
}

// TestLoadKeybindingsComeFromTheUserLayerOnly replaces a test that pinned the project
// object replacing the user's wholesale (Fresh's rule): a project file cannot rebind keys.
func TestLoadKeybindingsComeFromTheUserLayerOnly(t *testing.T) {
	dir := t.TempDir()
	userFile := filepath.Join(dir, "user.json")
	root := filepath.Join(dir, "proj")
	writeFile(t, userFile, `{"keybindings": {"a": "x", "b": "y"}}`)
	writeFile(t, filepath.Join(root, ".gvim", "config.json"), `{"keybindings": {"c": "z"}}`)
	cfg, _, err := Load(Options{Root: root, UserFile: userFile})
	if err != nil {
		t.Fatal(err)
	}
	if !jsonEqual(t, cfg.Keybindings, `{"a":"x","b":"y"}`) {
		t.Fatalf("keybindings = %s, want the user object only", cfg.Keybindings)
	}
}

func TestLoadMalformedFileKeepsDefaultsAndReports(t *testing.T) {
	dir := t.TempDir()
	userFile := filepath.Join(dir, "config.json")
	root := filepath.Join(dir, "proj")
	writeFile(t, userFile, `{"theme": "light", "tab_size": `)
	writeFile(t, filepath.Join(root, ".gvim", "config.json"), `{"line_numbers": false}`)

	cfg, diags, err := Load(Options{Root: root, UserFile: userFile})
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 1 {
		t.Fatalf("diags = %v, want one for the user file", diags)
	}
	if diags[0].Path != userFile || diags[0].Key != "" {
		t.Fatalf("diagnostic = %+v, want path %s with no key", diags[0], userFile)
	}
	if !strings.Contains(diags[0].String(), userFile) {
		t.Errorf("String() = %q, want the path", diags[0].String())
	}
	// The malformed user layer is skipped, so its theme is not applied. The
	// project layer still applies.
	if cfg.Theme != "dark" {
		t.Errorf("theme = %q, want default after malformed user layer", cfg.Theme)
	}
	if cfg.LineNumbers {
		t.Errorf("line_numbers = true, want false from the project layer")
	}
}

func TestLoadMalformedInputsNeverPanic(t *testing.T) {
	inputs := map[string]string{
		"array":          `[1, 2]`,
		"scalar":         `42`,
		"unterminated":   `{"theme": "x"`,
		"garbage":        `{{{`,
		"bare words":     `theme: dark`,
		"only comment":   `// nothing here`,
		"null top level": `null`,
		"empty object":   `{}`,
		"empty file":     ``,
		"whitespace":     "  \n\t ",
	}
	for name, content := range inputs {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			writeFile(t, path, content)
			cfg, _, err := Load(Options{UserFile: path})
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Theme == "" || cfg.TabSize == 0 {
				t.Fatalf("cfg = %+v, want defaults filled in", cfg)
			}
		})
	}
}

func TestLoadBadValuesAreDiagnosedPerKey(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		key     string
		wantMsg string
	}{
		{"tab size too large", `{"tab_size": 99}`, "tab_size", "between 1 and 16"},
		{"tab size zero", `{"tab_size": 0}`, "tab_size", "between 1 and 16"},
		{"tab size string", `{"tab_size": "4"}`, "tab_size", "must be an integer"},
		{"line ending", `{"line_ending": "lf2"}`, "line_ending", "must be"},
		{"bool as string", `{"auto_indent": "yes"}`, "auto_indent", "must be true or false"},
		{"menu bar as string", `{"show_menu_bar": "yes"}`, "show_menu_bar", "must be true or false"},
		{"empty theme", `{"theme": ""}`, "theme", "must not be empty"},
		{"theme not a string", `{"theme": 1}`, "theme", "must be a string"},
		{"null value", `{"line_numbers": null}`, "line_numbers", "null"},
		{"keybindings string", `{"keybindings": "ctrl+s"}`, "keybindings", "must be a list of bindings"},
		{"unknown key", `{"tabsize": 2}`, "tabsize", "unknown setting"},
		{"plugin timeout zero", `{"plugin_timeout_ms": 0}`, "plugin_timeout_ms", "between 1 and 60000"},
		{"plugin timeout too large", `{"plugin_timeout_ms": 60001}`, "plugin_timeout_ms", "between 1 and 60000"},
		{"plugin timeout string", `{"plugin_timeout_ms": "500"}`, "plugin_timeout_ms", "must be an integer"},
		{"plugin timeout fraction", `{"plugin_timeout_ms": 1.5}`, "plugin_timeout_ms", "must be an integer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			writeFile(t, path, tt.body)
			cfg, diags, err := Load(Options{UserFile: path})
			if err != nil {
				t.Fatal(err)
			}
			if len(diags) != 1 || diags[0].Key != tt.key || !strings.Contains(diags[0].Message, tt.wantMsg) {
				t.Fatalf("diags = %+v, want one for key %q containing %q", diags, tt.key, tt.wantMsg)
			}
			// Each body sets only the bad key, so every setting keeps its default.
			if !reflect.DeepEqual(cfg, Defaults()) {
				t.Fatalf("cfg = %+v, want defaults after a bad %q", cfg, tt.key)
			}
		})
	}
}

func TestLoadPluginTimeout(t *testing.T) {
	tests := []struct {
		name string
		body string
		want time.Duration
	}{
		{"default when unset", `{"theme": "dark"}`, 500 * time.Millisecond},
		{"milliseconds", `{"plugin_timeout_ms": 1500}`, 1500 * time.Millisecond},
		{"smallest allowed", `{"plugin_timeout_ms": 1}`, time.Millisecond},
		{"largest allowed", `{"plugin_timeout_ms": 60000}`, 60 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			writeFile(t, path, tt.body)
			cfg, diags, err := Load(Options{UserFile: path})
			if err != nil || len(diags) != 0 {
				t.Fatalf("Load = %v, %v, want no diagnostics", diags, err)
			}
			if cfg.PluginTimeout != tt.want {
				t.Fatalf("PluginTimeout = %v, want %v", cfg.PluginTimeout, tt.want)
			}
		})
	}
}

func TestLoadShowMenuBar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, `{"show_menu_bar": false}`)
	cfg, diags, err := Load(Options{UserFile: path})
	if err != nil || len(diags) != 0 {
		t.Fatalf("Load = %v, %v", diags, err)
	}
	if cfg.ShowMenuBar {
		t.Fatal("show_menu_bar false was not applied")
	}
}

func TestSourceNamesTheFileThatSetKey(t *testing.T) {
	dir := t.TempDir()
	userFile := filepath.Join(dir, "user.json")
	root := filepath.Join(dir, "proj")
	projFile := filepath.Join(root, ".gvim", "config.json")
	writeFile(t, userFile, `{"keymap": "emacs", "theme": "light", "keybindings": {"bindings": []}, "tab_size": 99}`)
	writeFile(t, projFile, `{"theme": "mono", "keybindings": {"bindings": [{"key": "s", "modifiers": ["ctrl"], "action": "noop"}]}}`)
	cfg, diags, err := Load(Options{Root: root, UserFile: userFile})
	if err != nil {
		t.Fatal(err)
	}
	// The bad tab_size, and the project file's keybindings, which a project cannot set.
	if len(diags) != 2 || diags[0].Key != "tab_size" || diags[1].Key != "keybindings" {
		t.Fatalf("diags = %+v, want the bad tab_size and the refused project keybindings", diags)
	}
	tests := []struct {
		key  string
		want string
	}{
		{"keymap", userFile},
		{"theme", projFile},       // the project layer replaces the user value
		{"keybindings", userFile}, // the project layer cannot set it
		{"tab_size", ""},          // rejected in the user file, so the default stands
		{"line_numbers", ""},      // set by no file
	}
	for _, tt := range tests {
		if got := cfg.Source(tt.key); got != tt.want {
			t.Errorf("Source(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestLoadValidKeyAfterBadKeyStillApplies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, `{"tab_size": 0, "theme": "mono"}`)
	cfg, diags, err := Load(Options{UserFile: path})
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 1 || diags[0].Key != "tab_size" {
		t.Fatalf("diags = %+v", diags)
	}
	if cfg.TabSize != 4 || cfg.Theme != "mono" {
		t.Fatalf("cfg = %+v, want tab_size default and theme mono", cfg)
	}
}

func TestLoadAcceptsDeclaredValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, `{"tab_size": 16, "line_ending": "lf", "trim_trailing_whitespace": true,
		"ensure_final_newline": true, "explorer_show_hidden": true, "explorer_show_ignored": true}`)
	cfg, diags, err := Load(Options{UserFile: path})
	if err != nil || len(diags) != 0 {
		t.Fatalf("Load = %v, %v", diags, err)
	}
	if cfg.TabSize != 16 || cfg.LineEnding != "lf" || !cfg.TrimTrailingWhitespace ||
		!cfg.EnsureFinalNewline || !cfg.ExplorerShowHidden || !cfg.ExplorerShowIgnored {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadDiagnosticsOrderUserBeforeProject(t *testing.T) {
	dir := t.TempDir()
	userFile := filepath.Join(dir, "user.json")
	root := filepath.Join(dir, "proj")
	writeFile(t, userFile, `{"tab_size": 0}`)
	projFile := filepath.Join(root, ".gvim", "config.json")
	writeFile(t, projFile, `{"line_ending": "lf2"}`)
	_, diags, err := Load(Options{Root: root, UserFile: userFile})
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 2 || diags[0].Path != userFile || diags[1].Path != projFile {
		t.Fatalf("diags = %+v, want user then project", diags)
	}
}

func TestFindUpNearestAncestorWins(t *testing.T) {
	// Each directory is checked before its parent, so the nearest file wins.
	present := map[string]bool{
		filepath.Join("/", "a", ".gvim", "config.json"):      true,
		filepath.Join("/", "a", "b", ".gvim", "config.json"): true,
	}
	got, ok := findUp(filepath.Join("/", "a", "b", "c"), func(p string) bool { return present[p] })
	if !ok || got != filepath.Join("/", "a", "b", ".gvim", "config.json") {
		t.Fatalf("findUp = %q, %v; want the nearer /a/b file", got, ok)
	}
}

func TestFindUpStopsAtFilesystemRoot(t *testing.T) {
	// The fake filesystem has a config file only above the start, at the root,
	// which findUp must still check. It must not look past the root.
	root := string(filepath.Separator)
	start := filepath.Join(root, "x", "y", "z")
	var visited []string
	got, ok := findUp(start, func(p string) bool {
		visited = append(visited, filepath.Dir(filepath.Dir(p)))
		return false
	})
	if ok || got != "" {
		t.Fatalf("findUp = %q, %v; want no file", got, ok)
	}
	want := []string{
		filepath.Join(root, "x", "y", "z"),
		filepath.Join(root, "x", "y"),
		filepath.Join(root, "x"),
		root,
	}
	if len(visited) != len(want) {
		t.Fatalf("visited %v, want %v", visited, want)
	}
	for i := range want {
		if visited[i] != want[i] {
			t.Fatalf("visited[%d] = %q, want %q (all: %v)", i, visited[i], want[i], visited)
		}
	}
}

func TestProjectFileSearchesUpward(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".gvim", "config.json")
	writeFile(t, cfgPath, `{}`)
	deep := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	got, ok, err := ProjectFile(deep)
	if err != nil || !ok || got != cfgPath {
		t.Fatalf("ProjectFile(%q) = %q, %v, %v; want %q", deep, got, ok, err, cfgPath)
	}
}

func TestProjectFileNoneFound(t *testing.T) {
	// The temp directory is outside any project that has a .gvim directory.
	// The search runs to the filesystem root and finds nothing, so ok is false.
	dir := t.TempDir()
	got, ok, err := ProjectFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("ProjectFile(%q) found %q; the temp tree should have no project file", dir, got)
	}
}

func TestProjectFileIgnoresDirectoryNamedConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".gvim", "config.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, ok, _ := ProjectFile(dir); ok {
		t.Fatalf("ProjectFile counted a directory: %q", got)
	}
}

func TestPathsUsesUserConfigDir(t *testing.T) {
	isolateHome(t)
	p, err := Paths()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p.User, filepath.Join("gvim", "config.json")) {
		t.Fatalf("Paths().User = %q, want .../gvim/config.json", p.User)
	}
}

func TestLoadReadsDefaultUserFileUnderHome(t *testing.T) {
	isolateHome(t)
	p, err := Paths()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, p.User, `{"tab_size": 2}`)
	cfg, diags, err := Load(Options{})
	if err != nil || len(diags) != 0 {
		t.Fatalf("Load = %v, %v", diags, err)
	}
	if cfg.TabSize != 2 {
		t.Fatalf("tab_size = %d, want 2 from the default user file", cfg.TabSize)
	}
}

func TestDiagnosticString(t *testing.T) {
	tests := []struct {
		d    Diagnostic
		want string
	}{
		{Diagnostic{Path: "/c.json", Key: "tab_size", Message: "bad"}, "/c.json: tab_size: bad"},
		{Diagnostic{Path: "/c.json", Message: "invalid JSON"}, "/c.json: invalid JSON"},
		{Diagnostic{Message: "no dir"}, "no dir"},
	}
	for _, tt := range tests {
		if got := tt.d.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
	}
}

func TestLoadLSPServers(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		want     map[string][]LSPServer
		wantDiag string // part of the diagnostic message for key lsp; empty when none
	}{
		{
			name: "command and args",
			body: `{"lsp": {"go": {"command": "gopls", "args": ["-remote=auto"]}}}`,
			want: map[string][]LSPServer{"go": {{Command: "gopls", Args: []string{"-remote=auto"}}}},
		},
		{
			name: "args are optional",
			body: `{"lsp": {"rust": {"command": "rust-analyzer"}}}`,
			want: map[string][]LSPServer{"rust": {{Command: "rust-analyzer"}}},
		},
		{
			name: "empty map starts no server",
			body: `{"lsp": {}}`,
			want: map[string][]LSPServer{},
		},
		{
			name:     "missing command",
			body:     `{"lsp": {"go": {"args": []}}}`,
			wantDiag: "command must not be empty",
		},
		{
			name:     "empty command",
			body:     `{"lsp": {"go": {"command": ""}}}`,
			wantDiag: "command must not be empty",
		},
		{
			name:     "unknown field",
			body:     `{"lsp": {"go": {"command": "gopls", "cmd": "x"}}}`,
			wantDiag: `unknown field "cmd"`,
		},
		{
			name:     "args not a list",
			body:     `{"lsp": {"go": {"command": "gopls", "args": "x"}}}`,
			wantDiag: "args",
		},
		{
			name:     "not an object",
			body:     `{"lsp": "gopls"}`,
			wantDiag: "must be a JSON object",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			writeFile(t, path, tt.body)
			cfg, diags, err := Load(Options{UserFile: path})
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantDiag == "" {
				if len(diags) != 0 {
					t.Fatalf("diags = %v, want none", diags)
				}
				if !reflect.DeepEqual(cfg.LSP, tt.want) {
					t.Fatalf("LSP = %+v, want %+v", cfg.LSP, tt.want)
				}
				return
			}
			if len(diags) != 1 || diags[0].Key != "lsp" || !strings.Contains(diags[0].Message, tt.wantDiag) {
				t.Fatalf("diags = %+v, want one for key lsp containing %q", diags, tt.wantDiag)
			}
			if cfg.LSP != nil {
				t.Fatalf("LSP = %+v after a bad lsp key, want none", cfg.LSP)
			}
		})
	}
}

// TestLoadLSPProjectLayerCannotRunAProgram checks the rule that a repository does not choose
// what program gvim starts: the project file may switch a language server off, or turn a
// built-in one on, but its command and args are refused with a diagnostic, and the user's own
// entries stay.
func TestLoadLSPProjectLayerCannotRunAProgram(t *testing.T) {
	const user = `{"lsp": {"go": {"command": "my-gopls"}, "rust": {"command": "ra", "args": ["-x"]}}}`
	tests := []struct {
		name         string
		project      string
		want         map[string][]LSPServer
		wantDisabled map[string]bool
		wantDiags    int
	}{
		{"a command is refused", `{"lsp": {"python": {"command": "touch", "args": ["/tmp/pwned"]}}}`,
			map[string][]LSPServer{"go": {{Command: "my-gopls"}}, "rust": {{Command: "ra", Args: []string{"-x"}}}}, nil, 1},
		{"a command cannot replace the user's", `{"lsp": {"go": {"command": "touch"}}}`,
			map[string][]LSPServer{"go": {{Command: "my-gopls"}}, "rust": {{Command: "ra", Args: []string{"-x"}}}}, nil, 1},
		{"args alone are refused too", `{"lsp": {"rust": {"args": ["-c", "x"], "enabled": true}}}`,
			map[string][]LSPServer{"go": {{Command: "my-gopls"}}, "rust": {{Command: "ra", Args: []string{"-x"}}}}, nil, 1},
		{"a project can switch a language off", `{"lsp": {"go": {"enabled": false}}}`,
			map[string][]LSPServer{"go": {{Command: "my-gopls"}}, "rust": {{Command: "ra", Args: []string{"-x"}}}}, map[string]bool{"go": true}, 0},
		{"a project can turn a built-in server on", `{"lsp": {"odin": {"enabled": true}}}`,
			map[string][]LSPServer{"go": {{Command: "my-gopls"}}, "rust": {{Command: "ra", Args: []string{"-x"}}}, "odin": {{}}}, nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			userFile := filepath.Join(dir, "user.json")
			writeFile(t, userFile, user)
			root := filepath.Join(dir, "proj")
			writeFile(t, filepath.Join(root, ".gvim", "config.json"), tt.project)
			cfg, diags, err := Load(Options{Root: root, UserFile: userFile})
			if err != nil {
				t.Fatal(err)
			}
			if len(diags) != tt.wantDiags {
				t.Fatalf("diags = %v, want %d", diags, tt.wantDiags)
			}
			for _, d := range diags {
				if d.Key != "lsp" || !strings.Contains(d.Message, "user config") {
					t.Errorf("diagnostic %+v does not tell the user to set it in the user config", d)
				}
			}
			if !reflect.DeepEqual(cfg.LSP, tt.want) || !reflect.DeepEqual(cfg.LSPDisabled, tt.wantDisabled) {
				t.Fatalf("LSP = %+v disabled %v, want %+v disabled %v", cfg.LSP, cfg.LSPDisabled, tt.want, tt.wantDisabled)
			}
		})
	}
}

func TestLoadLSPAutoStartAndRootMarkers(t *testing.T) {
	cfg, diags := loadBody(t, `{"lsp": {
		"go": {"auto_start": false},
		"rust": {"command": "ra", "auto_start": true, "root_markers": ["Cargo.toml", ".git"]}}}`)
	if len(diags) != 0 {
		t.Fatalf("diags = %v", diags)
	}
	want := map[string][]LSPServer{
		"go":   {{Dormant: true}},
		"rust": {{Command: "ra", RootMarkers: []string{"Cargo.toml", ".git"}}},
	}
	if !reflect.DeepEqual(cfg.LSP, want) {
		t.Fatalf("LSP = %+v, want %+v", cfg.LSP, want)
	}
}

// jsonEqual reports whether a and b are the same JSON value after decoding.
func jsonEqual(t *testing.T, a json.RawMessage, b string) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatalf("decode %s: %v", a, err)
	}
	if err := json.Unmarshal([]byte(b), &y); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	xb, _ := json.Marshal(x)
	yb, _ := json.Marshal(y)
	return string(xb) == string(yb)
}

func TestDirIsDotConfigGvimOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses os.UserConfigDir")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	t.Setenv("XDG_CONFIG_HOME", "")
	if got, err := Dir(); err != nil || got != filepath.Join(home, ".config", "gvim") {
		t.Errorf("Dir() = %q, %v, want ~/.config/gvim (no Library/Application Support on macOS)", got, err)
	}
	xdg := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if got, err := Dir(); err != nil || got != filepath.Join(xdg, "gvim") {
		t.Errorf("Dir() = %q, %v, want gvim under XDG_CONFIG_HOME", got, err)
	}
	t.Setenv("XDG_CONFIG_HOME", "relative/dir")
	if got, err := Dir(); err != nil || got != filepath.Join(home, ".config", "gvim") {
		t.Errorf("Dir() = %q, %v, want a relative XDG_CONFIG_HOME ignored", got, err)
	}
	files, err := Paths()
	if err != nil || files.User != filepath.Join(home, ".config", "gvim", "config.json") {
		t.Errorf("Paths().User = %q, %v", files.User, err)
	}
}

func TestStateDirIsXDGStateGvim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses os.UserConfigDir")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	if got, err := StateDir(); err != nil || got != filepath.Join(home, ".local", "state", "gvim") {
		t.Errorf("StateDir() = %q, %v, want ~/.local/state/gvim", got, err)
	}
	xdg := filepath.Join(t.TempDir(), "state")
	t.Setenv("XDG_STATE_HOME", xdg)
	if got, err := StateDir(); err != nil || got != filepath.Join(xdg, "gvim") {
		t.Errorf("StateDir() = %q, %v, want gvim under XDG_STATE_HOME", got, err)
	}
	t.Setenv("XDG_STATE_HOME", "relative/dir")
	if got, err := StateDir(); err != nil || got != filepath.Join(home, ".local", "state", "gvim") {
		t.Errorf("StateDir() = %q, %v, want a relative XDG_STATE_HOME ignored", got, err)
	}
}

func TestCacheDirIsGvim(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", base)
	t.Setenv("HOME", t.TempDir())
	got, err := CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, "gvim") {
		t.Errorf("CacheDir() = %q, want a gvim directory", got)
	}
}

func TestMoveLegacyFile(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old", "state.json")
	newPath := filepath.Join(dir, "new", "state.json")
	writeFile(t, oldPath, "old")
	if err := MoveLegacyFile(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(newPath); err != nil || string(data) != "old" {
		t.Fatalf("new file = %q, %v", data, err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old file still exists: %v", err)
	}
	// A file already at the new path wins and the old one is left alone.
	writeFile(t, oldPath, "stale")
	writeFile(t, newPath, "current")
	if err := MoveLegacyFile(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(newPath); string(data) != "current" {
		t.Fatalf("new file overwritten: %q", data)
	}
}
