package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/config"
)

// The stdin surface: the bounded read, the language choice, the validation, the session
// plan and the buffer buildRuntime opens.

func TestReadStdinIsBounded(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		limit   int64
		want    string
		wantErr string
	}{
		{"text", "hello\nworld\n", 64, "hello\nworld\n", ""},
		{"empty", "", 64, "", ""},
		{"exactly at the cap", "abc", 3, "abc", ""},
		{"over the cap", "abcd", 3, "", "stdin: input exceeds 0 MiB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readStdin(strings.NewReader(tt.input), tt.limit)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), "stdin: input exceeds") {
					t.Fatalf("readStdin error = %v, want the size error", err)
				}
				return
			}
			if err != nil || string(got) != tt.want {
				t.Fatalf("readStdin = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
	// A read error is reported with the stdin prefix.
	_, err := readStdin(errorReader{}, 8)
	if err == nil || !strings.Contains(err.Error(), "stdin:") {
		t.Errorf("read error = %v", err)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestStdinLanguage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		opts Options
		want string
	}{
		{"name wins", Options{StdinName: "notes.md", Stdin: []byte("x")}, "markdown"},
		{"go by name", Options{StdinName: "main.go"}, "go"},
		{"shebang sniff", Options{Stdin: []byte("#!/bin/bash\necho hi\n")}, "bash"},
		{"git diff sniff", Options{Stdin: []byte("diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+new\n")}, "diff"},
		{"plain words", Options{Stdin: []byte("just some words in a row\n")}, ""},
		{"empty", Options{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stdinLanguage(tt.opts); got != tt.want {
				t.Errorf("stdinLanguage = %q, want %q", got, tt.want)
			}
		})
	}
	if !stdinHasNUL([]byte("a\x00b")) || stdinHasNUL([]byte("ab")) {
		t.Error("stdinHasNUL is wrong")
	}
}

func TestValidateStdin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		opts    Options
		wantErr string
	}{
		{"stdin alone", Options{Stdin: []byte("x")}, ""},
		{"stdin with files", Options{Stdin: []byte("x"), Files: []FileTarget{{Path: "a.txt"}}}, ""},
		{"stdin with diff", Options{Stdin: []byte("x"), Diff: true, Files: []FileTarget{{Path: "a"}, {Path: "b"}}}, "--stdin cannot be combined with --diff"},
		{"stdin with restore", Options{ReadStdin: true, Restore: true}, "--restore cannot be combined with --stdin"},
		{"name without stdin", Options{StdinName: "a.go"}, "--stdin-name needs --stdin or a - argument"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// TestPlanSessionTreatsStdinAsNoSessionButNotFreshFileLaunch: gvim keeps every stdin
// launch out of the session (its piped text is not session state), while Fresh's
// is_file_launch does not count standalone stdin; files combined with stdin still follow
// Fresh's file condition.
func TestPlanSessionTreatsStdinAsNoSessionButNotFreshFileLaunch(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	for _, tt := range []struct {
		name           string
		opts           Options
		wantFileLaunch bool
	}{
		{"stdin content", Options{Stdin: []byte("x"), SessionDir: "/s"}, false},
		{"read flag", Options{ReadStdin: true, SessionDir: "/s"}, false},
		{"name", Options{StdinName: "a.md", SessionDir: "/s"}, false},
		// Explicit files are still Fresh's file launch even alongside stdin.
		{"stdin with files", Options{Stdin: []byte("x"), Files: []FileTarget{{Path: "a.txt"}}, SessionDir: "/s"}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			plan := planSession(tt.opts, cfg)
			if plan.fileLaunch != tt.wantFileLaunch {
				t.Errorf("fileLaunch = %v, want %v (Fresh is_file_launch excludes stdin)", plan.fileLaunch, tt.wantFileLaunch)
			}
			if plan.load || plan.save || plan.hotExit || plan.tabs || plan.hot {
				t.Errorf("plan = %+v, want no session (stdin is not session state)", plan)
			}
		})
	}
	// A stdin launch with editor.skip_session_restore_when_files_passed=false is still
	// sessionless; standalone stdin is still not a Fresh file launch.
	restoreAlways := cfg
	restoreAlways.SkipSessionRestoreWhenFilesPassed = false
	plan := planSession(Options{Stdin: []byte("x"), SessionDir: "/s"}, restoreAlways)
	if plan.fileLaunch || plan.load || plan.save {
		t.Errorf("plan = %+v, want no Fresh file launch and no session", plan)
	}
	// --restore wins over the file condition just as Fresh's is_file_launch does; the
	// stdin session exclusion is independent.
	plan = planSession(Options{Stdin: []byte("x"), Restore: true, Files: []FileTarget{{Path: "a.txt"}}, SessionDir: "/s"}, cfg)
	if plan.fileLaunch || plan.load || plan.save {
		t.Errorf("plan = %+v, want --restore to keep Fresh fileLaunch false and stdin sessionless", plan)
	}
}

func buildStdinRuntime(t *testing.T, opts Options) *runtime {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	if opts.Root == "" {
		opts.Root = t.TempDir()
	}
	if opts.SessionDir == "" {
		opts.SessionDir = t.TempDir()
	}
	opts.Simple = true
	opts.NoPlugins = true
	rt, err := buildRuntime(context.Background(), opts, false)
	if err != nil {
		t.Fatalf("buildRuntime: %v", err)
	}
	t.Cleanup(func() { rt.close() })
	return rt
}

func TestBuildRuntimeOpensTheStdinBuffer(t *testing.T) {
	rt := buildStdinRuntime(t, Options{Stdin: []byte("hello from stdin\nsecond line\n"), StdinName: "notes.md"})
	tabs := rt.ed.Tabs()
	if len(tabs) != 1 || tabs[0].Name != "[stdin]" || !tabs[0].Active {
		t.Fatalf("tabs = %+v, want one active [stdin] tab", tabs)
	}
	doc := rt.ed.Active()
	if doc == nil || doc.Path != "" || doc.Modified() || doc.Saved != "hello from stdin\nsecond line\n" {
		t.Fatalf("active doc = %+v (saved %q)", doc, doc.Saved)
	}
	if got := doc.Language(); got != "markdown" {
		t.Errorf("language = %q, want markdown from --stdin-name", got)
	}
}

func TestBuildRuntimeStdinBeforeFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt := buildStdinRuntime(t, Options{Stdin: []byte("piped\n"), Files: []FileTarget{{Path: path}}})
	names := func() []string {
		var out []string
		for _, tab := range rt.ed.Tabs() {
			out = append(out, tab.Name)
		}
		return out
	}
	if got := names(); !slices.Equal(got, []string{"[stdin]", "a.txt"}) {
		t.Errorf("tabs = %v, want the stdin buffer before the files", got)
	}
	if doc := rt.ed.Active(); doc == nil || doc.Name() != "a.txt" {
		t.Errorf("active = %v, want the file argument", doc.Name())
	}
}

// TestReadStdinContextStopsOnCancel checks a pipe that never closes does not hold the
// launch once the context ends (Ctrl-C while "tail -f x | gvim" waits).
func TestReadStdinContextStopsOnCancel(t *testing.T) {
	pr, pw := io.Pipe()
	defer func() { _ = pw.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := readStdinContext(ctx, pr, MaxStdinBytes)
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want the cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the read did not stop when the context ended")
	}
}
