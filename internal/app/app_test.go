package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateConfig keeps the recent list out of the real user config directory.
func isolateConfig(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", home)
	// ui_icons "auto" reads the terminal from these, so a test run inside kitty, WezTerm
	// or Ghostty would otherwise start with Nerd Font icons.
	for _, k := range []string{"KITTY_WINDOW_ID", "WEZTERM_EXECUTABLE", "GHOSTTY_RESOURCES_DIR", "TERM_PROGRAM", "TERM"} {
		t.Setenv(k, "")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		opts    Options
		wantErr string
	}{
		{name: "zero value", opts: Options{}},
		{name: "every built-in keymap", opts: Options{Keymap: "macos-gui"}},
		{name: "emacs keymap", opts: Options{Keymap: "emacs"}},
		{name: "bundled theme", opts: Options{Theme: "dark"}},
		{name: "file with line and column", opts: Options{Files: []FileTarget{{Path: "a.go", Line: 3, Col: 2}}}},
		{name: "missing file is valid", opts: Options{Files: []FileTarget{{Path: "new.txt"}}}},
		{name: "plugin options", opts: Options{NoPlugins: true, PluginPaths: []string{"a.lua", "dir"}}},
		{name: "empty plugin path", opts: Options{PluginPaths: []string{"a.lua", " "}}, wantErr: "--plugin: path is empty"},
		{name: "unknown keymap", opts: Options{Keymap: "vim"}, wantErr: `unknown keymap "vim"`},
		{name: "unknown theme", opts: Options{Theme: "neon"}, wantErr: `unknown theme "neon"`},
		{name: "empty file path", opts: Options{Files: []FileTarget{{Path: ""}}}, wantErr: "file path is empty"},
		{name: "negative line", opts: Options{Files: []FileTarget{{Path: "a.go", Line: -1}}}, wantErr: "must not be negative"},
		{name: "negative column", opts: Options{Files: []FileTarget{{Path: "a.go", Line: 1, Col: -2}}}, wantErr: "must not be negative"},
		{name: "column without line", opts: Options{Files: []FileTarget{{Path: "a.go", Col: 4}}}, wantErr: "a column needs a line"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestBuildModelOpensFilesAndMissingFiles(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := Options{
		Files: []FileTarget{
			{Path: path, Line: 2, Col: 1},
			{Path: filepath.Join(dir, "new.txt")},
		},
		Root: dir,
	}
	if _, _, err := buildModel(t.Context(), opts); err != nil {
		t.Fatalf("buildModel() error = %v", err)
	}
}

func TestBuildModelRejectsUndecodableFile(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	// A UTF-16LE byte-order mark, then a lone high surrogate: no text decodes, so
	// the editor refuses the file.
	bad := filepath.Join(dir, "bad.bin")
	if err := os.WriteFile(bad, []byte{0xff, 0xfe, 0x00, 0xd8}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := buildModel(t.Context(), Options{Files: []FileTarget{{Path: bad}}, Root: dir}); err == nil {
		t.Fatal("buildModel() with an undecodable file returned no error")
	}
}

func TestBuildModelRejectsMissingRoot(t *testing.T) {
	isolateConfig(t)
	if _, _, err := buildModel(t.Context(), Options{Root: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("buildModel() with a missing root returned no error")
	}
}

func TestBuildModelUnknownKeymapAndTheme(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	if _, _, err := buildModel(t.Context(), Options{Keymap: "nope", Root: dir}); err == nil {
		t.Error("unknown keymap: no error")
	}
	if _, _, err := buildModel(t.Context(), Options{Theme: "nope", Root: dir}); err == nil {
		t.Error("unknown theme: no error")
	}
}

func TestResolveRootEmptyIsCurrentDirectory(t *testing.T) {
	isolateConfig(t)
	want, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolveRoot("")
	if err != nil {
		t.Fatalf("resolveRoot(\"\") error = %v", err)
	}
	if got != want {
		t.Fatalf("resolveRoot(\"\") = %q, want %q", got, want)
	}
}

func TestResolveRootDirectoryIsResolved(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolveRoot(dir)
	if err != nil {
		t.Fatalf("resolveRoot(%q) error = %v", dir, err)
	}
	if got != want {
		t.Fatalf("resolveRoot(%q) = %q, want %q", dir, got, want)
	}
}

func TestKeymapForDefaultsToPlatformKeymap(t *testing.T) {
	def, err := keymapFor("")
	if err != nil {
		t.Fatal(err)
	}
	if def == nil {
		t.Fatal("keymapFor(\"\") = nil")
	}
	named, err := keymapFor("default")
	if err != nil {
		t.Fatalf("keymapFor(default) error = %v", err)
	}
	if named == nil {
		t.Fatal("keymapFor(default) = nil")
	}
}
