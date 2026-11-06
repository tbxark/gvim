package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/tbxark/gvim/internal/app"
	"github.com/tbxark/gvim/internal/config"
	"github.com/tbxark/gvim/internal/keymap"
)

// runTree runs a command tree with args and returns its stdout, its stderr and
// the error from the command. Usage and error printing by cobra are turned off,
// so only the command's own output is captured.
func runTree(t *testing.T, cmd *cobra.Command, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd.SetArgs(args)
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestConfigShowWithFakeLoader(t *testing.T) {
	const settings = "{\n  \"theme\": \"light\"\n}\n"
	tests := []struct {
		name       string
		view       app.ConfigView
		loadErr    error
		cwdErr     error
		wantLoaded bool
		wantOut    string
		wantStderr string
		wantErr    string
	}{
		{
			name:       "prints the loaded settings",
			view:       app.ConfigView{JSON: []byte(settings)},
			wantLoaded: true,
			wantOut:    settings,
		},
		{
			name: "warnings go to stderr and do not fail",
			view: app.ConfigView{
				JSON:     []byte(settings),
				Warnings: []string{"/p/.gvim/config.json: tab_size: must be between 1 and 16, got 99", "/u/config.json: invalid JSON"},
			},
			wantLoaded: true,
			wantOut:    settings,
			wantStderr: "gvim: warning: /p/.gvim/config.json: tab_size: must be between 1 and 16, got 99\n" +
				"gvim: warning: /u/config.json: invalid JSON\n",
		},
		{
			name:       "loader error fails",
			loadErr:    errors.New("disk gone"),
			wantLoaded: true,
			wantErr:    "disk gone",
		},
		{
			name:    "working directory error fails before loading",
			cwdErr:  errors.New("no working directory"),
			wantErr: "no working directory",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotRoot string
			loaded := false
			d := configDeps{
				cwd: func() (string, error) {
					if tt.cwdErr != nil {
						return "", tt.cwdErr
					}
					return "/work/proj", nil
				},
				load: func(root string, _ app.Options) (app.ConfigView, error) {
					loaded = true
					gotRoot = root
					return tt.view, tt.loadErr
				},
			}
			stdout, stderr, err := runTree(t, newConfigCommandWith(d, new(string), new(string), new(string)), "show")
			if loaded != tt.wantLoaded {
				t.Fatalf("loader called = %v, want %v", loaded, tt.wantLoaded)
			}
			if tt.wantLoaded && gotRoot != "/work/proj" {
				t.Fatalf("loader root = %q, want the working directory", gotRoot)
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("error = %v", err)
			}
			if stdout != tt.wantOut {
				t.Fatalf("stdout = %q, want %q", stdout, tt.wantOut)
			}
			if stderr != tt.wantStderr {
				t.Fatalf("stderr = %q, want %q", stderr, tt.wantStderr)
			}
		})
	}
}

func TestConfigPathsWithFakeFiles(t *testing.T) {
	tests := []struct {
		name  string
		files []app.ConfigFile
		want  string
	}{
		{
			name: "both present",
			files: []app.ConfigFile{
				{Label: "user", Path: "/home/u/gvim/config.json", Exists: true},
				{Label: "project", Path: "/work/.gvim/config.json", Exists: true},
			},
			want: "user     /home/u/gvim/config.json (exists)\nproject  /work/.gvim/config.json (exists)\n",
		},
		{
			name: "both missing",
			files: []app.ConfigFile{
				{Label: "user", Path: "/home/u/gvim/config.json"},
				{Label: "project", Path: "/work/.gvim/config.json"},
			},
			want: "user     /home/u/gvim/config.json (missing)\nproject  /work/.gvim/config.json (missing)\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := configDeps{
				cwd:   func() (string, error) { return "/work", nil },
				paths: func(string, string) ([]app.ConfigFile, error) { return tt.files, nil },
			}
			stdout, stderr, err := runTree(t, newConfigCommandWith(d, new(string), new(string), new(string)), "paths")
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if stdout != tt.want {
				t.Fatalf("stdout = %q, want %q", stdout, tt.want)
			}
			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
		})
	}
}

func TestConfigPathsErrorFails(t *testing.T) {
	d := configDeps{
		cwd:   func() (string, error) { return "/work", nil },
		paths: func(string, string) ([]app.ConfigFile, error) { return nil, errors.New("no home") },
	}
	stdout, _, err := runTree(t, newConfigCommandWith(d, new(string), new(string), new(string)), "paths")
	if err == nil || !strings.Contains(err.Error(), "no home") {
		t.Fatalf("error = %v, want it to contain %q", err, "no home")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
}

// TestConfigFlagValueReachesDeps checks that the value of --config is the path
// that show and paths pass to their dependencies.
func TestConfigFlagValueReachesDeps(t *testing.T) {
	var loadPath, pathsPath string
	d := configDeps{
		cwd: func() (string, error) { return "/work", nil },
		load: func(_ string, opts app.Options) (app.ConfigView, error) {
			loadPath = opts.Config
			return app.ConfigView{JSON: []byte("{}\n")}, nil
		},
		paths: func(_, configPath string) ([]app.ConfigFile, error) {
			pathsPath = configPath
			return nil, nil
		},
	}
	flagPath := "/etc/gvim.json"
	cmd := newConfigCommandWith(d, &flagPath, new(string), new(string))
	if _, _, err := runTree(t, cmd, "show"); err != nil {
		t.Fatalf("show error = %v", err)
	}
	if loadPath != flagPath {
		t.Fatalf("load got config %q, want %q", loadPath, flagPath)
	}
	if _, _, err := runTree(t, cmd, "paths"); err != nil {
		t.Fatalf("paths error = %v", err)
	}
	if pathsPath != flagPath {
		t.Fatalf("paths got config %q, want %q", pathsPath, flagPath)
	}
}

// TestConfigPathsOutput runs the real command against temporary HOME and XDG
// directories. Placeholders are filled with the real locations for this run.
func TestConfigPathsOutput(t *testing.T) {
	tests := []struct {
		name string
		// setup writes the files for the case. base is the temporary parent
		// of the working directory, which is base/work/deep. userFile is the
		// user config file under the temporary HOME.
		setup       func(t *testing.T, base, userFile string)
		wantProject string // project path, relative to base
		want        string // expected output with {USER} and {PROJECT}
	}{
		{
			name:        "no files",
			wantProject: "work/deep/.gvim/config.json",
			want: "user     {USER} (missing)\n" +
				"project  {PROJECT} (missing)\n",
		},
		{
			name: "project file in the working directory",
			setup: func(t *testing.T, base, _ string) {
				writeFile(t, filepath.Join(base, "work", "deep", ".gvim", "config.json"), "{}")
			},
			wantProject: "work/deep/.gvim/config.json",
			want: "user     {USER} (missing)\n" +
				"project  {PROJECT} (exists)\n",
		},
		{
			name: "project file in a parent directory and user file present",
			setup: func(t *testing.T, base, userFile string) {
				writeFile(t, filepath.Join(base, ".gvim", "config.json"), "{}")
				writeFile(t, userFile, "{}")
			},
			wantProject: ".gvim/config.json",
			want: "user     {USER} (exists)\n" +
				"project  {PROJECT} (exists)\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateHome(t)
			userDir, err := config.Dir()
			if err != nil {
				t.Fatal(err)
			}
			userFile := filepath.Join(userDir, "config.json")

			base := t.TempDir()
			root := filepath.Join(base, "work", "deep")
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatal(err)
			}
			if tt.setup != nil {
				tt.setup(t, base, userFile)
			}
			t.Chdir(root)

			var stdout, stderr bytes.Buffer
			code := Execute(context.Background(), []string{"config", "paths"}, &stdout, &stderr, nil)
			if code != 0 {
				t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
			}
			want := strings.NewReplacer(
				"{USER}", userFile,
				"{PROJECT}", filepath.Join(base, filepath.FromSlash(tt.wantProject)),
			).Replace(tt.want)
			if got := stdout.String(); got != want {
				t.Fatalf("stdout =\n%s\nwant\n%s", got, want)
			}
		})
	}
}

// TestConfigShowReal runs the real command. The user file sets the theme and
// the project file sets the tab size and has one unknown key, which is
// reported on stderr and skipped.
func TestConfigShowReal(t *testing.T) {
	isolateHome(t)
	userDir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(userDir, "config.json"), `{"theme": "light", "tab_size": 2}`)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".gvim", "config.json"), `{"tab_size": 8, "bogus": true}`)
	t.Chdir(root)

	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"config", "show"}, &stdout, &stderr, nil)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	want := `{
  "theme": "light",
  "keymap": "` + keymap.DefaultName() + `",
  "tab_size": 8,
  "insert_spaces": true,
  "line_ending": "auto",
  "trim_trailing_whitespace": false,
  "ensure_final_newline": false,
  "auto_indent": true,
  "line_numbers": true,
  "show_menu_bar": true,
  "explorer_show_hidden": false,
  "explorer_show_ignored": false
}
`
	if got := stdout.String(); got != want {
		t.Fatalf("stdout =\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(stderr.String(), "gvim: warning: ") || !strings.Contains(stderr.String(), "bogus: unknown setting") {
		t.Fatalf("stderr = %q, want a warning about the bogus key", stderr.String())
	}
}

// TestConfigFlagReachesShowAndPaths runs the real command with --config. The
// alternate file replaces the default user file, so show prints its theme and
// paths lists it in place of the user file.
func TestConfigFlagReachesShowAndPaths(t *testing.T) {
	isolateHome(t)
	userDir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(userDir, "config.json"), `{"theme": "light"}`)
	alt := filepath.Join(t.TempDir(), "alt.json")
	writeFile(t, alt, `{"theme": "nord"}`)
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"--config", alt, "config", "show"}, &stdout, &stderr, nil)
	if code != 0 {
		t.Fatalf("show exit = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"theme": "nord"`) {
		t.Fatalf("show stdout = %s, want the theme from %s", stdout.String(), alt)
	}

	stdout.Reset()
	stderr.Reset()
	code = Execute(context.Background(), []string{"--config", alt, "config", "paths"}, &stdout, &stderr, nil)
	if code != 0 {
		t.Fatalf("paths exit = %d, stderr = %q", code, stderr.String())
	}
	if want := "user     " + alt + " (exists)\n"; !strings.HasPrefix(stdout.String(), want) {
		t.Fatalf("paths stdout = %q, want it to start with %q", stdout.String(), want)
	}
}

// TestConfigShowMissingFlagFileFails checks that a --config path that does not
// exist is an error that names the path, not a silent fall back to defaults.
func TestConfigShowMissingFlagFileFails(t *testing.T) {
	isolateHome(t)
	t.Chdir(t.TempDir())
	missing := filepath.Join(t.TempDir(), "absent.json")

	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"--config", missing, "config", "show"}, &stdout, &stderr, nil)
	if code == 0 {
		t.Fatalf("exit = 0, want failure; stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), missing) {
		t.Fatalf("stderr = %q, want it to name %s", stderr.String(), missing)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

// TestConfigShowFlagsWinOverConfig runs the real command. The --theme and
// --keymap flags name the settings the editor uses, so show prints them in place
// of the values in the user file.
func TestConfigShowFlagsWinOverConfig(t *testing.T) {
	isolateHome(t)
	userDir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(userDir, "config.json"), `{"theme": "light", "keymap": "vscode"}`)
	t.Chdir(t.TempDir())

	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"--theme", "nord", "--keymap", "emacs", "config", "show"}, &stdout, &stderr, nil)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	var got map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if got["theme"] != "nord" || got["keymap"] != "emacs" {
		t.Fatalf("theme = %v, keymap = %v; want nord and emacs from the flags\n%s", got["theme"], got["keymap"], stdout.String())
	}
}

// TestConfigShowEffectiveSettingsPrecedence checks the precedence the editor uses
// for theme and keymap: the flag, then the config file, then the default.
func TestConfigShowEffectiveSettingsPrecedence(t *testing.T) {
	tests := []struct {
		name       string
		userConfig string // written to the user file; empty writes no file
		args       []string
		wantTheme  string
		wantKeymap string
	}{
		{
			name:       "no flags, config sets both",
			userConfig: `{"theme": "light", "keymap": "vscode"}`,
			wantTheme:  "light",
			wantKeymap: "vscode",
		},
		{
			name:       "flags win over config",
			userConfig: `{"theme": "light", "keymap": "vscode"}`,
			args:       []string{"--theme", "nord", "--keymap", "emacs"},
			wantTheme:  "nord",
			wantKeymap: "emacs",
		},
		{
			name:       "theme flag alone keeps the config keymap",
			userConfig: `{"keymap": "vscode"}`,
			args:       []string{"--theme", "nord"},
			wantTheme:  "nord",
			wantKeymap: "vscode",
		},
		{
			name:       "keymap flag alone keeps the config theme",
			userConfig: `{"theme": "light"}`,
			args:       []string{"--keymap", "emacs"},
			wantTheme:  "light",
			wantKeymap: "emacs",
		},
		{
			name:       "no flags and no config use the defaults",
			wantTheme:  "dark",
			wantKeymap: keymap.DefaultName(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateHome(t)
			if tt.userConfig != "" {
				userDir, err := config.Dir()
				if err != nil {
					t.Fatal(err)
				}
				writeFile(t, filepath.Join(userDir, "config.json"), tt.userConfig)
			}
			t.Chdir(t.TempDir())

			var stdout, stderr bytes.Buffer
			args := append(append([]string{}, tt.args...), "config", "show")
			code := Execute(context.Background(), args, &stdout, &stderr, nil)
			if code != 0 {
				t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
			}
			var got map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
				t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
			}
			if got["theme"] != tt.wantTheme || got["keymap"] != tt.wantKeymap {
				t.Fatalf("theme = %v, keymap = %v; want %s and %s", got["theme"], got["keymap"], tt.wantTheme, tt.wantKeymap)
			}
		})
	}
}

// TestConfigShowRejectsUnknownFlagValue checks that show fails for a --theme or
// --keymap name the editor would also refuse, instead of printing it.
func TestConfigShowRejectsUnknownFlagValue(t *testing.T) {
	tests := []struct {
		args    []string
		wantErr string
	}{
		{args: []string{"--theme", "neon", "config", "show"}, wantErr: `unknown theme "neon"`},
		{args: []string{"--keymap", "vim", "config", "show"}, wantErr: `unknown keymap "vim"`},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			isolateHome(t)
			t.Chdir(t.TempDir())
			var stdout, stderr bytes.Buffer
			code := Execute(context.Background(), tt.args, &stdout, &stderr, nil)
			if code == 0 {
				t.Fatalf("exit = 0, want failure; stdout = %q", stdout.String())
			}
			if !strings.Contains(stderr.String(), tt.wantErr) {
				t.Fatalf("stderr = %q, want it to contain %q", stderr.String(), tt.wantErr)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
		})
	}
}

// writeFile creates path with content, making its parent directories.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
