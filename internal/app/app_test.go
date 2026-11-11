package app

import (
	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/internal/uitest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// isolateConfig keeps the recent list out of the real user config directory.
func isolateConfig(t *testing.T) {
	t.Helper()
	home := tempHome(t)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("AppData", home)
	// Session tests predate editor.save_session defaulting to off: the isolated user
	// config enables it, like a user who turned saving on. The disabled default has
	// its own tests in config and internal/app/project_session_storage_test.go.
	userConfig := filepath.Join(home, ".config", "gvim", "config.json")
	if err := os.MkdirAll(filepath.Dir(userConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userConfig, []byte(`{"editor":{"save_session":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// ui_icons "auto" reads the terminal from these, so a test run inside kitty, WezTerm
	// or Ghostty would otherwise start with Nerd Font icons.
	for _, k := range []string{"KITTY_WINDOW_ID", "WEZTERM_EXECUTABLE", "GHOSTTY_RESOURCES_DIR", "TERM_PROGRAM", "TERM"} {
		t.Setenv(k, "")
	}
}

// tempHome creates an isolated HOME for a test. Unlike t.TempDir it tolerates the
// cleanup race with spawned helper processes: gopls and the go toolchain it runs can
// still be writing their build cache under HOME when the test ends, so the tree is
// removed with a few bounded retries and a residual directory is ignored.
func tempHome(t *testing.T) string {
	t.Helper()
	home, err := os.MkdirTemp("", "gvim-home-")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOCACHE", filepath.Join(home, ".cache", "go-build"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Cleanup(func() {
		for range 20 {
			if err := os.RemoveAll(home); err == nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		_ = os.RemoveAll(home)
	})
	return home
}

func TestValidate(t *testing.T) {
	isolateConfig(t)
	// A user theme named neon exists, so the named form is valid.
	userThemes := userThemeDir()
	if err := os.MkdirAll(userThemes, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userThemes, "neon.json"), []byte(`{"name":"neon","extends":"builtin://dark"}`), 0o644); err != nil {
		t.Fatal(err)
	}
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
		// A plain name resolves against the bundled themes and the user theme dir.
		{name: "user theme name", opts: Options{Theme: "neon"}},
		{name: "unknown theme name", opts: Options{Theme: "not-a-theme"}, wantErr: `unknown theme "not-a-theme"`},
		{name: "user theme path", opts: Options{Theme: "subdir/dark.json"}},
		{name: "file theme", opts: Options{Theme: "file:///tmp/x.json"}},
		{name: "https URL theme", opts: Options{Theme: "https://example.com/t.json"}},
		{name: "loopback http URL theme", opts: Options{Theme: "http://127.0.0.1:8080/t.json"}},
		{name: "plain http URL theme", opts: Options{Theme: "http://example.com/t.json"}, wantErr: "refusing the plain-http"},
		{name: "URL-packaged theme with a fragment", opts: Options{Theme: "https://example.com/themes#dark"}, wantErr: "packaged repository themes"},
		{name: "empty file path", opts: Options{Files: []FileTarget{{Path: ""}}}, wantErr: "file path is empty"},
		{name: "negative line", opts: Options{Files: []FileTarget{{Path: "a.go", Line: -1}}}, wantErr: "must not be negative"},
		{name: "negative column", opts: Options{Files: []FileTarget{{Path: "a.go", Line: 1, Col: -2}}}, wantErr: "must not be negative"},
		{name: "column without line", opts: Options{Files: []FileTarget{{Path: "a.go", Col: 4}}}, wantErr: "a column needs a line"},
		{name: "restore with no-restore", opts: Options{Restore: true, NoRestore: true}, wantErr: "--restore cannot be combined with --no-restore"},
		{name: "restore with simple", opts: Options{Restore: true, Simple: true}, wantErr: "--restore cannot be combined with --simple"},
		{name: "restore with diff", opts: Options{Restore: true, Diff: true, Files: []FileTarget{{Path: "a"}, {Path: "b"}}}, wantErr: "--restore cannot be combined with --diff"},
		{name: "simple with no-restore", opts: Options{Simple: true, NoRestore: true}},
		{name: "restore alone", opts: Options{Restore: true}},
		{name: "simple alone", opts: Options{Simple: true}},
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

// TestValidateRemote checks the remote-launch rules of app.Options.
func TestValidateRemote(t *testing.T) {
	spec := &authority.RemoteSpec{User: "u", Host: "h", Path: "/srv"}
	tests := []struct {
		name    string
		opts    Options
		wantErr string
	}{
		{"remote alone", Options{Remote: spec}, ""},
		{"remote with file", Options{Remote: spec, Files: []FileTarget{{Path: "a.go"}}}, "local directories or files"},
		{"remote with root", Options{Remote: spec, Root: "dir"}, "local directories or files"},
		{"remote with diff", Options{Remote: spec, Diff: true}, "--diff"},
		{"remote with restore", Options{Remote: spec, Restore: true}, "does not restore"},
		{"remote with stdin", Options{Remote: spec, ReadStdin: true}, "--stdin"},
		{"remote with daemon", Options{Remote: spec, Daemon: &DaemonOptions{Mode: DaemonServe}}, ""},
		{"remote with attach", Options{Remote: spec, Daemon: &DaemonOptions{Mode: DaemonAttach}}, ""},
		{"remote empty host", Options{Remote: &authority.RemoteSpec{User: "u", Path: "/x"}}, "host is empty"},
		{"remote empty user", Options{Remote: &authority.RemoteSpec{Host: "h", Path: "/x"}}, "user is empty"},
		{"remote empty path", Options{Remote: &authority.RemoteSpec{User: "u", Host: "h"}}, "path is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// TestBuildModelReadOnlyProtectsFiles verifies that launching with Options.ReadOnly
// protects opened files in code-viewer mode against typed edits.
func TestBuildModelReadOnlyProtectsFiles(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "view.txt")
	if err := os.WriteFile(path, []byte("code to read\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, files, err := buildModel(t.Context(), Options{
		Root:     dir,
		Files:    []FileTarget{{Path: path}},
		ReadOnly: true,
	})
	if err != nil {
		t.Fatalf("buildModel() error = %v", err)
	}
	t.Cleanup(func() { files.Close() })
	s := uitest.Start(t, m, 100, 30)
	s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("code to read") })
	s.Type("XYZ")
	s.Settle()
	sc := s.Screen()
	if sc.Contains("XYZ") {
		t.Fatalf("read-only mode accepted typed edits: %s", sc.String())
	}
}
