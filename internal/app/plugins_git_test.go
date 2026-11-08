package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/decorations"
	"github.com/tbxark/gvim/internal/tui"
	expanel "github.com/tbxark/gvim/internal/tui/explorer"
)

// The bundled git ports against the real runtime, the real editor and a real repository
// made with the git binary (the test is skipped without one).

// gitIn runs git in dir with the user's configuration kept out.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// decorStoreSession starts a session and captures the decoration store of its editor port.
func decorStoreSession(t *testing.T, opts Options) (*pluginSession, *decorations.Store) {
	t.Helper()
	var store *decorations.Store
	prev := newUI
	newUI = func(d tui.Deps) *tui.Model {
		if src, ok := d.Root.PluginEditor.(interface{ DecorStore() *decorations.Store }); ok {
			store = src.DecorStore()
		}
		return prev(d)
	}
	t.Cleanup(func() { newUI = prev })
	s := newSession(t, opts)
	// The statusbar plugin holds a watch on the repository: stop the plugins with the test,
	// or the goroutine of the watch outlives it (TestQuittingUnloadsThePluginsCleanly counts them).
	t.Cleanup(s.rt.exit)
	if store == nil {
		t.Fatal("no decoration store")
	}
	return s, store
}

// gutterMarks returns the lines that carry a gutter bar of the git_gutter plugin, with
// their text.
func gutterMarks(st *decorations.Store, doc uint64) map[int]string {
	out := map[int]string{}
	for _, it := range st.Items(doc, "git_gutter", "git_gutter") {
		out[it.FromLine] = it.Text
	}
	return out
}

func TestGitPortsOnARealRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git binary")
	}
	isolateConfig(t)
	repo := t.TempDir()
	path := filepath.Join(repo, "a.txt")
	must(t, os.WriteFile(path, []byte("one\ntwo\nthree\nfour\n"), 0o644))
	gitIn(t, repo, "init")
	gitIn(t, repo, "checkout", "-b", "gvim-test-branch")
	gitIn(t, repo, "add", "a.txt")
	gitIn(t, repo, "commit", "-m", "first")

	s, store := decorStoreSession(t, Options{Root: repo})
	for _, name := range []string{"git_gutter", "git_statusbar"} {
		s.waitStatus(name, "loaded")
	}
	s.send(expanel.OpenFileMsg{Path: path})
	s.until("the file", func() bool { return s.rt.ed.Active() != nil && s.rt.ed.Active().Path == path })
	doc := uint64(s.rt.ed.Active().ID())

	// The branch is on the status row.
	s.until("the branch on the status row", func() bool { return strings.Contains(s.statusRow(), "gvim-test-branch") })
	if got := gutterMarks(store, doc); len(got) != 0 {
		t.Fatalf("marks on an unchanged file: %v", got)
	}

	// Typing at the start of the first line changes it: the bar appears after the pause.
	s.typeText("X")
	s.until("a change bar on line 0", func() bool { return gutterMarks(store, doc)[0] == "│" })
	if got := gutterMarks(store, doc); len(got) != 1 {
		t.Errorf("marks = %v, want only line 0", got)
	}
	if n := len(store.Items(doc, "git_gutter", "git_gutter_scroll")); n != 1 {
		t.Errorf("%d scrollbar marks, want 1", n)
	}

	// A new line at the end is "added", and a save keeps the bars (HEAD is read again).
	s.press(tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl})
	s.typeText("five")
	s.until("a bar on the added line", func() bool { return len(gutterMarks(store, doc)) == 2 })
	s.press(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	s.until("the save", func() bool { return !s.rt.ed.Active().Modified() })
	s.settle(50 * time.Millisecond)
	if got := gutterMarks(store, doc); len(got) != 2 || got[0] != "│" {
		t.Errorf("marks after the save = %v", got)
	}

	// Undoing everything brings the buffer back to HEAD: the bars go.
	for range 40 {
		if s.text() == "one\ntwo\nthree\nfour\n" {
			break
		}
		s.press(tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	}
	s.until("the bars to go", func() bool { return len(gutterMarks(store, doc)) == 0 })
	if p := s.pluginProblems("git_gutter"); len(p) != 0 {
		t.Errorf("git_gutter problems: %v", p)
	}
}

// TestGitPortsCommandsOnARealRepository runs the blame and find-file commands from the
// palette against real git output.
func TestGitPortsCommandsOnARealRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git binary")
	}
	isolateConfig(t)
	repo := t.TempDir()
	path := filepath.Join(repo, "a.txt")
	must(t, os.WriteFile(path, []byte("one\ntwo\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(repo, "b.go"), []byte("package b\n"), 0o644))
	gitIn(t, repo, "init")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-m", "first commit")

	s, store := decorStoreSession(t, Options{Root: repo})
	for _, name := range []string{"git_blame", "git_find_file"} {
		s.waitStatus(name, "loaded")
	}
	s.send(expanel.OpenFileMsg{Path: path})
	s.until("the file", func() bool { return s.rt.ed.Active() != nil && s.rt.ed.Active().Path == path })
	doc := uint64(s.rt.ed.Active().ID())

	s.runCommand("Git: Toggle Blame")
	blame := func() []decorations.Item { return store.Items(doc, "git_blame", "blame") }
	s.until("the blame text", func() bool { return len(blame()) == 1 })
	if it := blame()[0]; it.Kind != decorations.EOL || it.FromLine != 0 || it.Text != "  t, just now \u2022 first commit" {
		t.Errorf("blame item = %+v", it)
	}

	s.runCommand("Git Find File")
	s.until("the picker", func() bool { return s.screenHas("Find file") && s.screenHas("b.go") })
	if p := s.pluginProblems("git_blame"); len(p) != 0 {
		t.Errorf("git_blame problems: %v", p)
	}
}

// pluginProblems returns the warnings and errors a plugin logged.
func (s *pluginSession) pluginProblems(name string) []string {
	var out []string
	for _, e := range s.plug.Log() {
		if e.Plugin == name && (e.Level == "warn" || e.Level == "error") {
			out = append(out, e.Msg)
		}
	}
	return out
}

func TestGitPortsStaySilentOutsideARepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git binary")
	}
	isolateConfig(t)
	// A temp dir is not inside a repository unless the machine's temp directory is in one.
	dir := t.TempDir()
	if err := exec.Command("git", "-C", dir, "rev-parse").Run(); err == nil {
		t.Skip("the temp directory is inside a repository")
	}
	path := filepath.Join(dir, "a.txt")
	must(t, os.WriteFile(path, []byte("one\ntwo\n"), 0o644))

	s, store := decorStoreSession(t, Options{Root: dir})
	for _, name := range []string{"git_gutter", "git_statusbar", "git_find_file", "git_blame"} {
		s.waitStatus(name, "loaded")
	}
	s.send(expanel.OpenFileMsg{Path: path})
	s.until("the file", func() bool { return s.rt.ed.Active() != nil && s.rt.ed.Active().Path == path })
	s.typeText("X")
	s.pump(500 * time.Millisecond)

	doc := uint64(s.rt.ed.Active().ID())
	if n := store.Count(doc); n != 0 {
		t.Errorf("%d decorations outside a repository", n)
	}
	for _, name := range []string{"git_gutter", "git_statusbar", "git_find_file", "git_blame"} {
		if p := s.pluginProblems(name); len(p) != 0 {
			t.Errorf("%s problems: %v", name, p)
		}
	}
}
