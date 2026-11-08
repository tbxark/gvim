package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/finder"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/uiadapter"
	"github.com/tbxark/gvim/internal/uitest"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/theme"
)

// startProjectSearchSession runs the real UI over root with the project-search ports
// wired, on the file named file.
func startProjectSearchSession(t *testing.T, root, file string, w, h int) *uitest.Session {
	t.Helper()
	ed := editor.New(editor.Options{Rows: h - 3, Cols: w})
	if err := ed.OpenFile(filepath.Join(root, file)); err != nil {
		t.Fatal(err)
	}
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := explorer.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	index := finder.Start(context.Background(), root)
	t.Cleanup(index.Close)
	search := newProjectSearch(editor.Options{})
	deps := uiadapter.NewDeps(ed, keymap.Default(), th, tree, index)
	deps.Root.ProjectSearch = search
	deps.Root.ProjectReplace = search
	deps.Root.GitGrep = gitGrep{}
	return uitest.Start(t, tui.New(deps), w, h)
}

// TestProjectSearchPanelEndToEnd: Alt+A opens the panel, the query finds the project's
// matches, Alt+Enter confirmed replaces them across files, the changed files stay as
// unsaved tabs, and the notice asks for Save All. The disk is not written.
func TestProjectSearchPanelEndToEnd(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"a.txt": "foo one\n",
		"b.txt": "two foo\n",
	})
	s := startProjectSearchSession(t, root, "a.txt", 110, 32)
	s.Key("alt+a")
	s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Search & Replace") })
	s.Type("foo")
	s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("2 matches in 2 files") })
	s.Key("tab") // the replacement field
	s.Type("bar")
	s.Key("alt+enter")
	sc := s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Replace 2 matches in 2 files?") })
	if !sc.Contains("[ No ]") {
		t.Fatalf("the confirmation does not offer No:\n%s", sc)
	}
	s.Key("y")
	sc = s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Save All to write") })
	if !strings.Contains(sc.String(), "a.txt") || !strings.Contains(sc.String(), "b.txt") {
		t.Fatalf("the changed files are not open tabs:\n%s", sc)
	}
	// The replacement is in the unsaved tabs only: the deliberate divergence from
	// Fresh's write-to-disk behaviour.
	for name, want := range map[string]string{"a.txt": "foo one\n", "b.txt": "two foo\n"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != want {
			t.Errorf("%s on disk = %q, want it unchanged %q", name, data, want)
		}
	}
}

// TestGitGrepCommandRowOpensThePanel: the native command palette row opens the Git Grep
// panel read-only.
func TestGitGrepCommandRowOpensThePanel(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a.txt": "foo\n"})
	s := startProjectSearchSession(t, root, "a.txt", 110, 32)
	s.Key("ctrl+p")
	s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Commands") })
	s.Type("git grep")
	s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Git Grep") })
	s.Key("enter")
	sc := s.WaitFor(func(sc uitest.Screen) bool { return sc.Contains("Alt+C case") })
	if !strings.Contains(sc.String(), "Git Grep") {
		t.Fatalf("the Git Grep panel did not open:\n%s", sc)
	}
}
