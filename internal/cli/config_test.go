package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/tbxark/gvim/internal/app"
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
				load: func(root string) (app.ConfigView, error) {
					loaded = true
					gotRoot = root
					return tt.view, tt.loadErr
				},
			}
			stdout, stderr, err := runTree(t, newConfigCommandWith(d), "show")
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
				paths: func(string) ([]app.ConfigFile, error) { return tt.files, nil },
			}
			stdout, stderr, err := runTree(t, newConfigCommandWith(d), "paths")
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
		paths: func(string) ([]app.ConfigFile, error) { return nil, errors.New("no home") },
	}
	stdout, _, err := runTree(t, newConfigCommandWith(d), "paths")
	if err == nil || !strings.Contains(err.Error(), "no home") {
		t.Fatalf("error = %v, want it to contain %q", err, "no home")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
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
			userDir, err := os.UserConfigDir()
			if err != nil {
				t.Fatal(err)
			}
			userFile := filepath.Join(userDir, "gvim", "config.json")

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
	userDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(userDir, "gvim", "config.json"), `{"theme": "light", "tab_size": 2}`)
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
