// Package apptest holds the test helpers the application's packages share: a
// temporary root, a helper that fails a test on an error, and the isolation of
// the user config directory. It is test support only: no production package may
// import it.
package apptest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/editor"
)

// Must fails the test when err is not nil.
func Must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// IsolateConfig keeps the recent list and the config files out of the real user
// config directory.
func IsolateConfig(t *testing.T) {
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

// WriteTestFile writes content to path.
func WriteTestFile(t *testing.T, path, content string) {
	t.Helper()
	Must(t, os.MkdirAll(filepath.Dir(path), 0o755))
	Must(t, os.WriteFile(path, []byte(content), 0o644))
}

// BigLines writes a file of 5.5 MiB, which is over the 4 MiB that opening a large
// file indexes at once. Every 1000th line holds "needle".
func BigLines(t *testing.T, dir string) string {
	t.Helper()
	var sb strings.Builder
	for i := 0; sb.Len() < 5<<20+512<<10; i++ {
		if i%1000 == 0 {
			fmt.Fprintf(&sb, "%d needle %030d\n", i, 0)
			continue
		}
		fmt.Fprintf(&sb, "%039d\n", i)
	}
	path := filepath.Join(dir, "big.txt")
	Must(t, os.WriteFile(path, []byte(sb.String()), 0o644))
	return path
}

// OpenBig opens the file at path in an editor with large file mode on at 1 KiB.
func OpenBig(t *testing.T, path string) *editor.Editor {
	t.Helper()
	ed := editor.New(editor.Options{Empty: true, LargeFileMode: true, LargeFileThresholdBytes: 1024})
	Must(t, ed.OpenFile(path))
	t.Cleanup(ed.Close)
	if !ed.Active().Large() {
		t.Fatal("the document is not large")
	}
	return ed
}

// NewSessionEditor returns an editor with no documents, as gvim starts without file
// arguments.
func NewSessionEditor() *editor.Editor {
	return editor.New(editor.Options{Empty: true})
}

// TabNames returns the names of the tabs of ed, in order, and the active index.
func TabNames(ed *editor.Editor) ([]string, int) {
	var names []string
	active := -1
	for i, t := range ed.Tabs() {
		names = append(names, t.Name)
		if t.Active {
			active = i
		}
	}
	return names, active
}

// LargeOpts are the editor options of the session tests: a 1 KiB threshold makes a
// file of a few KiB large.
func LargeOpts() editor.Options {
	return editor.Options{Empty: true, LargeFileMode: true, LargeFileThresholdBytes: 1024}
}

// LargeSessionFile writes a file of 250 lines, opens it in a large-mode editor, makes
// two edits far apart, and returns the editor and the path.
func LargeSessionFile(t *testing.T, dir string) (*editor.Editor, string) {
	t.Helper()
	var sb strings.Builder
	for i := range 250 {
		sb.WriteString("line ")
		sb.WriteString(strings.Repeat("x", i%5))
		sb.WriteString("\n")
	}
	path := filepath.Join(dir, "big.txt")
	Must(t, os.WriteFile(path, []byte(sb.String()), 0o644))
	ed := editor.New(LargeOpts())
	Must(t, ed.OpenFile(path))
	d := ed.Active()
	if !d.Large() {
		t.Fatal("the document is not large")
	}
	for !d.Index().Complete {
		job, ok := ed.PlanIndex()
		if !ok {
			t.Fatal("no index job for an incomplete index")
		}
		ed.ApplyIndex(job.Run(t.Context()))
	}
	Must(t, ed.GotoLine(200, 1))
	ed.Do("insert_char", map[string]any{"text": "deep "})
	Must(t, ed.GotoLine(3, 3))
	ed.Do("insert_char", map[string]any{"text": "ABC"})
	ed.Do("insert_newline", nil)
	return ed, path
}

// IndexComplete runs the index jobs of ed until its index is complete.
func IndexComplete(t *testing.T, ed *editor.Editor) {
	t.Helper()
	for {
		job, ok := ed.PlanIndex()
		if !ok {
			return
		}
		if !ed.ApplyIndex(job.Run(t.Context())) {
			return
		}
	}
}

// StashFiles lists the text stash files in dir.
func StashFiles(t *testing.T, dir string) []string {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, "stash-*.txt"))
	Must(t, err)
	return names
}
