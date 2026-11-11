package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/restore"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/tui/find"
	"github.com/tbxark/gvim/session"
)

// newSessionEditor returns an editor with no documents, as gvim starts without
// file arguments.

// tabNames returns the names of the tabs of ed, in order, and the active index.

// TestSessionRoundTripKeepsTabsCursorsActiveAndUnsavedText saves a session with two
// files, one of them edited and not saved, and restores it into a new editor. The
// tabs, the active tab, the cursor of the first file and the unsaved text all come
// back.

// TestSessionSkipsSavedFilesThatAreGone checks that a file that was deleted since the
// session was saved is not reopened as an empty tab, and that the active tab still
// lands on the file that is there.

// TestSessionRestoresUnsavedUntitledText checks that an untitled buffer with text is
// kept by the session and comes back as an untitled buffer with that text.

// TestSessionDropsEmptyUntitledTabs checks that an untitled buffer with no text is
// not kept, so the session does not grow an empty tab each time it is saved.

// TestBuildRuntimeRestoresTheSessionWhenNoFilesGiven checks the start-up path: with
// no file arguments the saved tabs open, and the saved explorer settings apply.
func TestBuildRuntimeRestoresTheSessionWhenNoFilesGiven(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	must(t, os.WriteFile(file, []byte("a\n"), 0o644))
	sessDir := filepath.Join(dir, "session")
	ed := apptest.NewSessionEditor()
	must(t, ed.OpenFile(file))
	must(t, restore.SaveSession(ed, sessDir, tui.ExplorerState{Open: true, ShowHidden: true, ShowIgnored: true}))

	rt, err := buildRuntime(t.Context(), Options{SessionDir: sessDir}, false)
	must(t, err)
	defer rt.close()
	names, _ := apptest.TabNames(rt.ed)
	if len(names) != 1 || names[0] != "a.txt" {
		t.Fatalf("tabs at start = %v, want a.txt from the session", names)
	}
	st := rt.model.ExplorerState()
	if !st.Open || !st.ShowHidden || !st.ShowIgnored {
		t.Fatalf("explorer state at start = %+v, want the saved settings", st)
	}
}

func TestHiddenFilesWithRestoredSession(t *testing.T) {
	for _, tc := range []struct {
		name string
		show bool
		want bool
	}{
		{"session remembers hidden", false, false},
		{"session remembers shown", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfig(t)
			root := t.TempDir()
			must(t, os.WriteFile(filepath.Join(root, ".example"), []byte("test"), 0o644))
			dir := filepath.Join(root, "session")
			must(t, session.Save(dir, session.Session{Root: root, ShowHidden: tc.show}))
			rt, err := buildRuntime(t.Context(), Options{Root: root, SessionDir: dir}, false)
			must(t, err)
			defer rt.close()
			if got := rt.model.ExplorerState().ShowHidden; got != tc.want {
				t.Fatalf("show hidden = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestBuildRuntimeWithFilesDoesNotRestore checks that file arguments win: the session
// is not opened when gvim is given files.
func TestBuildRuntimeWithFilesDoesNotRestore(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	saved := filepath.Join(dir, "saved.txt")
	given := filepath.Join(dir, "given.txt")
	must(t, os.WriteFile(saved, []byte("s\n"), 0o644))
	must(t, os.WriteFile(given, []byte("g\n"), 0o644))
	sessDir := filepath.Join(dir, "session")
	ed := apptest.NewSessionEditor()
	must(t, ed.OpenFile(saved))
	must(t, restore.SaveSession(ed, sessDir, tui.ExplorerState{}))

	rt, err := buildRuntime(t.Context(), Options{SessionDir: sessDir, Files: []FileTarget{{Path: given}}}, false)
	must(t, err)
	defer rt.close()
	names, _ := apptest.TabNames(rt.ed)
	if len(names) != 1 || names[0] != "given.txt" {
		t.Fatalf("tabs = %v, want only given.txt", names)
	}
}

// TestBuildRuntimeWithFilesLeavesTheSavedSessionAlone checks the exit save of a start
// with file arguments. The saved session is not restored, so its unsaved text is not
// in the editor. The save at exit must not replace session.json or remove the stash
// file that the saved session names.
func TestBuildRuntimeWithFilesLeavesTheSavedSessionAlone(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	saved := filepath.Join(dir, "saved.txt")
	given := filepath.Join(dir, "given.txt")
	must(t, os.WriteFile(saved, []byte("s\n"), 0o644))
	must(t, os.WriteFile(given, []byte("g\n"), 0o644))
	sessDir := filepath.Join(dir, "session")
	stash, err := session.StashContent(sessDir, "unsaved edits")
	must(t, err)
	must(t, session.Save(sessDir, session.Session{Files: []session.FileState{{Path: saved, StashFile: stash}}}))
	sessionJSON := filepath.Join(sessDir, "session.json")
	stashPath := filepath.Join(sessDir, stash)
	wantSession, err := os.ReadFile(sessionJSON)
	must(t, err)
	wantStash, err := os.ReadFile(stashPath)
	must(t, err)

	rt, err := buildRuntime(t.Context(), Options{SessionDir: sessDir, Files: []FileTarget{{Path: given}}}, false)
	must(t, err)
	defer rt.close()
	must(t, rt.save())

	if got, err := os.ReadFile(sessionJSON); err != nil || !bytes.Equal(got, wantSession) {
		t.Fatalf("session.json after the exit save = %q (error %v), want it unchanged", got, err)
	}
	if got, err := os.ReadFile(stashPath); err != nil || !bytes.Equal(got, wantStash) {
		t.Fatalf("stash after the exit save = %q (error %v), want it unchanged", got, err)
	}
}

// TestRuntimeSaveWritesTheSessionAtExit checks what Run does when the program ends
// after a start without file arguments: the runtime writes the session of its editor to
// the session directory. A start with file arguments writes no session, as
// TestBuildRuntimeWithFilesLeavesTheSavedSessionAlone checks.
func TestRuntimeSaveWritesTheSessionAtExit(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	must(t, os.WriteFile(file, []byte("a\n"), 0o644))
	sessDir := filepath.Join(dir, "session")

	rt, err := buildRuntime(t.Context(), Options{SessionDir: sessDir}, false)
	must(t, err)
	defer rt.close()
	must(t, rt.ed.OpenFile(file))
	must(t, rt.save())

	s, ok, err := session.Load(sessDir)
	must(t, err)
	if !ok || len(s.Files) != 1 || s.Files[0].Path != file {
		t.Fatalf("saved session = %+v (found %v), want a.txt", s, ok)
	}
}

// TestRuntimeWiresTheRootFeatures checks that the root features are reachable from
// the built model: live grep opens its palette instead of the not-supported notice.
func TestRuntimeWiresTheRootFeatures(t *testing.T) {
	isolateConfig(t)
	rt, err := buildRuntime(t.Context(), Options{Root: t.TempDir()}, false)
	must(t, err)
	defer rt.close()
	rt.model.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	must(t, rt.model.Run("open_live_grep"))
	// The status row cuts the notice short, so the check looks for its start.
	if strings.Contains(ansi.Strip(rt.model.View().Content), "Live Grep is not supported") {
		t.Fatal("live grep is not wired: the model says it is not supported")
	}
}

// TestSearchHistoryAndTogglesSurviveARestart saves the find state of one runtime
// and starts another over the same directory, with file arguments, which skip the
// session itself.
func TestSearchHistoryAndTogglesSurviveARestart(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	must(t, os.WriteFile(file, []byte("foo bar\n"), 0o644))
	sessDir := filepath.Join(dir, "session")

	rt, err := buildRuntime(t.Context(), Options{SessionDir: sessDir, Files: []FileTarget{{Path: file}}}, false)
	must(t, err)
	rt.model.SetSearchState(find.State{Find: []string{"foo"}, Replace: []string{"baz"}, Regex: true})
	must(t, rt.saveSearch())
	rt.close()

	rt2, err := buildRuntime(t.Context(), Options{SessionDir: sessDir, Files: []FileTarget{{Path: file}}}, false)
	must(t, err)
	defer rt2.close()
	st, ok := rt2.model.SearchState()
	if !ok || len(st.Find) != 1 || st.Find[0] != "foo" || len(st.Replace) != 1 || st.Replace[0] != "baz" || !st.Regex {
		t.Fatalf("restored search state = %+v (%v)", st, ok)
	}
}
