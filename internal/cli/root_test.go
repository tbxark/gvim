package cli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/app"
)

// recorder is a fake Runner. It remembers the options of the last call and
// returns err.
type recorder struct {
	calls int
	got   app.Options
	err   error
}

func (r *recorder) run(_ context.Context, opts app.Options) error {
	r.calls++
	r.got = opts
	return r.err
}

// execute runs the command tree with args and returns the exit code, the
// captured output and the runner that was passed in.
func execute(t *testing.T, args []string, rec *recorder) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), args, &stdout, &stderr, rec.run)
	return code, stdout.String(), stderr.String()
}

// isolateHome points the user config and home directories at a temp dir, so
// that a directory argument does not write the real recent list.
func isolateHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("AppData", home)
}

func TestExecuteFlagsReachOptions(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	tests := []struct {
		name string
		args []string
		want app.Options
	}{
		{
			name: "no arguments",
			args: []string{},
			want: app.Options{},
		},
		{
			name: "keymap flag",
			args: []string{"--keymap", "emacs"},
			want: app.Options{Keymap: "emacs"},
		},
		{
			name: "theme flag",
			args: []string{"--theme", "dark"},
			want: app.Options{Theme: "dark"},
		},
		{
			name: "config and log file flags",
			args: []string{"--config", "/etc/gvim.jsonc", "--log-file", "/tmp/gvim.log"},
			want: app.Options{Config: "/etc/gvim.jsonc", LogFile: "/tmp/gvim.log"},
		},
		{
			name: "flags after positional arguments",
			args: []string{"a.txt", "--keymap", "vscode"},
			want: app.Options{Keymap: "vscode", Files: []app.FileTarget{{Path: "a.txt"}}},
		},
		{
			name: "file with position",
			args: []string{"missing.go:12:4"},
			want: app.Options{Files: []app.FileTarget{{Path: "missing.go", Line: 12, Col: 4}}},
		},
		{
			name: "read-only flag short",
			args: []string{"-R", "a.txt"},
			want: app.Options{ReadOnly: true, Files: []app.FileTarget{{Path: "a.txt"}}},
		},
		{
			name: "read-only flag long",
			args: []string{"--read-only", "a.txt"},
			want: app.Options{ReadOnly: true, Files: []app.FileTarget{{Path: "a.txt"}}},
		},
		{
			name: "view flag alias",
			args: []string{"--view", "a.txt"},
			want: app.Options{ReadOnly: true, Files: []app.FileTarget{{Path: "a.txt"}}},
		},
		{
			name: "no-plugins flag",
			args: []string{"--no-plugins"},
			want: app.Options{NoPlugins: true},
		},
		{
			name: "plugin flag is repeatable and keeps commas",
			args: []string{"--plugin", "/p/a.lua", "--plugin=/p/b,c"},
			want: app.Options{PluginPaths: []string{"/p/a.lua", "/p/b,c"}},
		},
		{
			name: "no-plugins with plugin and positional arguments",
			args: []string{"a.txt", "--plugin", "x.lua", "--no-plugins", "b.txt:2"},
			want: app.Options{
				NoPlugins:   true,
				PluginPaths: []string{"x.lua"},
				Files:       []app.FileTarget{{Path: "a.txt"}, {Path: "b.txt", Line: 2}},
			},
		},
		{
			name: "directory root and files",
			args: []string{dir, "a.txt", "b.txt:3"},
			want: app.Options{
				Root:  dir,
				Files: []app.FileTarget{{Path: "a.txt"}, {Path: "b.txt", Line: 3}},
			},
		},
		{
			name: "restore flag",
			args: []string{"--restore"},
			want: app.Options{Restore: true},
		},
		{
			name: "no-restore flag",
			args: []string{"--no-restore"},
			want: app.Options{NoRestore: true},
		},
		{
			name: "no-session is the no-restore alias",
			args: []string{"--no-session"},
			want: app.Options{NoRestore: true},
		},
		{
			name: "simple flag with a file",
			args: []string{"--simple", "a.txt"},
			want: app.Options{Simple: true, Files: []app.FileTarget{{Path: "a.txt"}}},
		},
		{
			name: "no-restore and simple together",
			args: []string{"--simple", "--no-restore"},
			want: app.Options{Simple: true, NoRestore: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{}
			code, _, stderr := execute(t, tt.args, rec)
			if code != 0 {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			if rec.calls != 1 {
				t.Fatalf("runner called %d times, want 1", rec.calls)
			}
			if !reflect.DeepEqual(rec.got, tt.want) {
				t.Fatalf("options = %+v, want %+v", rec.got, tt.want)
			}
		})
	}
}

func TestExecuteRejectsBadInputWithoutRunning(t *testing.T) {
	isolateHome(t)
	dir := t.TempDir()
	tests := []struct {
		name     string
		args     []string
		wantCode int
		wantErr  string
	}{
		{name: "unknown keymap", args: []string{"--keymap", "vim"}, wantCode: 1, wantErr: `unknown keymap "vim"`},
		{name: "unknown theme", args: []string{"--theme", "neon"}, wantCode: 1, wantErr: `unknown theme "neon"`},
		{name: "unknown flag", args: []string{"--no-such-flag"}, wantCode: 1, wantErr: "unknown flag"},
		{name: "missing flag value", args: []string{"--keymap"}, wantCode: 1, wantErr: "needs an argument"},
		{name: "empty plugin path", args: []string{"--plugin", ""}, wantCode: 1, wantErr: "--plugin: path is empty"},
		{name: "missing plugin value", args: []string{"--plugin"}, wantCode: 1, wantErr: "needs an argument"},
		{name: "no-plugins takes no value", args: []string{"--no-plugins=maybe"}, wantCode: 1, wantErr: "invalid argument"},
		{name: "two directories", args: []string{dir, t.TempDir()}, wantCode: 1, wantErr: "only one directory"},
		{name: "restore and no-restore", args: []string{"--restore", "--no-restore"}, wantCode: 1, wantErr: "--restore cannot be combined with --no-restore"},
		{name: "restore and no-session", args: []string{"--restore", "--no-session"}, wantCode: 1, wantErr: "--restore cannot be combined with --no-restore"},
		{name: "restore and simple", args: []string{"--restore", "--simple"}, wantCode: 1, wantErr: "--restore cannot be combined with --simple"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recorder{}
			code, _, stderr := execute(t, tt.args, rec)
			if code != tt.wantCode {
				t.Fatalf("exit = %d, want %d (stderr %q)", code, tt.wantCode, stderr)
			}
			if !strings.Contains(stderr, tt.wantErr) {
				t.Fatalf("stderr = %q, want it to contain %q", stderr, tt.wantErr)
			}
			if rec.calls != 0 {
				t.Fatalf("runner called %d times, want 0", rec.calls)
			}
		})
	}
}

func TestExecuteReportsRunnerErrorOnce(t *testing.T) {
	rec := &recorder{err: errors.New("no tty")}
	code, stdout, stderr := execute(t, []string{}, rec)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if want := "gvim: no tty\n"; stderr != want {
		t.Fatalf("stderr = %q, want %q", stderr, want)
	}
	if strings.Contains(stdout, "Usage:") || strings.Contains(stderr, "Usage:") {
		t.Fatalf("runtime error printed usage: stdout %q, stderr %q", stdout, stderr)
	}
}

func TestExecuteCanceledContextExits130Silently(t *testing.T) {
	rec := &recorder{err: context.Canceled}
	code, _, stderr := execute(t, []string{}, rec)
	if code != 130 {
		t.Fatalf("exit = %d, want 130", code)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
}

func TestConfigAndGrammarCommandsAreImplemented(t *testing.T) {
	for _, args := range [][]string{
		{"config", "paths"},
		{"grammar", "list"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			rec := &recorder{}
			code, _, stderr := execute(t, args, rec)
			if code != 0 {
				t.Fatalf("exit = %d, want 0 (stderr %q)", code, stderr)
			}
			if strings.Contains(stderr, "not yet implemented") {
				t.Fatalf("stderr still reports a stub: %q", stderr)
			}
		})
	}
}

func TestExecuteVersion(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			rec := &recorder{}
			code, stdout, stderr := execute(t, args, rec)
			if code != 0 {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			if !strings.Contains(stdout, "gvim version ") {
				t.Fatalf("stdout = %q, want a version line", stdout)
			}
			if rec.calls != 0 {
				t.Fatalf("runner called %d times, want 0", rec.calls)
			}
		})
	}
}

func TestExecuteVersionShowsBuildInfo(t *testing.T) {
	rec := &recorder{}
	_, stdout, _ := execute(t, []string{"version"}, rec)
	if !strings.Contains(stdout, "go go") || !strings.Contains(stdout, "platform ") {
		t.Fatalf("stdout = %q, want Go and platform lines", stdout)
	}
}

func TestExecuteHelpListsCommandTree(t *testing.T) {
	rec := &recorder{}
	code, stdout, stderr := execute(t, []string{"--help"}, rec)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{"version", "config", "grammar", "completion", "help", "--config", "--keymap", "--theme", "--log-file", "--no-plugins", "--plugin", "--restore", "--no-restore", "--no-session", "--simple"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help misses %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "nothing is written") || !strings.Contains(stdout, "plugin log to PATH") {
		t.Errorf("help must say what --log-file writes:\n%s", stdout)
	}
	if rec.calls != 0 {
		t.Fatalf("runner called %d times, want 0", rec.calls)
	}
}

// The completion generators in cobra write to os.Stdout directly, so the test
// only checks that the subcommand is registered and resolves.
func TestCompletionCommandIsRegistered(t *testing.T) {
	root := NewRootCommand(nil)
	for _, name := range []string{"completion", "version", "config", "grammar"} {
		cmd, _, err := root.Find([]string{name})
		if err != nil || cmd.Name() != name {
			t.Fatalf("Find(%q) = %v, %v", name, cmd, err)
		}
	}
}

func TestExecuteNilArgsDoesNotReadProcessArgs(t *testing.T) {
	// go test always passes -test.* flags in os.Args. They must not reach the
	// command tree when args is nil.
	rec := &recorder{}
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), nil, &stdout, &stderr, rec.run)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	if rec.calls != 1 || len(rec.got.Files) != 0 || rec.got.Root != "" {
		t.Fatalf("runner got %+v (calls %d), want empty options", rec.got, rec.calls)
	}
}

func TestExecuteWithNilRunnerFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{}, &stdout, &stderr, nil)
	if code != 1 || !strings.Contains(stderr.String(), "no runner configured") {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
}
