package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/session"
	"github.com/tbxark/gvim/internal/ui"
)

// newSessionEditor returns an editor with no documents, as gvim starts without
// file arguments.
func newSessionEditor() *editor.Editor {
	return editor.New(editor.Options{Empty: true})
}

// tabNames returns the names of the tabs of ed, in order, and the active index.
func tabNames(ed *editor.Editor) ([]string, int) {
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

// TestSessionRoundTripKeepsTabsCursorsActiveAndUnsavedText saves a session with two
// files, one of them edited and not saved, and restores it into a new editor. The
// tabs, the active tab, the cursor of the first file and the unsaved text all come
// back.
func TestSessionRoundTripKeepsTabsCursorsActiveAndUnsavedText(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	must(t, os.WriteFile(a, []byte("one\ntwo\nthree\n"), 0o644))
	must(t, os.WriteFile(b, []byte("bee\n"), 0o644))
	sessDir := filepath.Join(dir, "session")

	ed := newSessionEditor()
	must(t, ed.OpenFile(a))
	must(t, ed.GotoLine(2, 2))
	must(t, ed.OpenFile(b))
	if res := ed.Do("insert_char", map[string]any{"text": "Z"}); res.Err != nil {
		t.Fatal(res.Err)
	}
	must(t, saveSession(ed, sessDir, ui.ExplorerState{Open: true, ShowHidden: true}))

	restored := newSessionEditor()
	s, ok, err := session.Load(sessDir)
	must(t, err)
	if !ok {
		t.Fatal("no session was saved")
	}
	restoreSession(restored, sessDir, s)

	names, active := tabNames(restored)
	if len(names) != 2 || names[0] != "a.txt" || names[1] != "b.txt" {
		t.Fatalf("tabs = %v, want a.txt and b.txt", names)
	}
	if active != 1 {
		t.Fatalf("active tab = %d, want 1 (b.txt)", active)
	}
	if got := restored.Active().Buffer.Text(); got != "Zbee\n" || !restored.Active().Modified() {
		t.Fatalf("unsaved text = %q (modified %v), want Zbee modified", got, restored.Active().Modified())
	}
	restored.ActivateTab(0)
	if c := restored.Active().Cursor; c.Line != 1 || c.Col != 1 {
		t.Fatalf("cursor of a.txt = %+v, want line 2 column 2 (zero-based 1,1)", c)
	}
}

// TestSessionSkipsSavedFilesThatAreGone checks that a file that was deleted since the
// session was saved is not reopened as an empty tab, and that the active tab still
// lands on the file that is there.
func TestSessionSkipsSavedFilesThatAreGone(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.txt")
	must(t, os.WriteFile(keep, []byte("k\n"), 0o644))
	s := session.Session{Files: []session.FileState{
		{Path: filepath.Join(dir, "gone.txt"), Line: 1, Col: 1},
		{Path: keep, Line: 1, Col: 1},
	}, Active: 1}
	ed := newSessionEditor()
	restoreSession(ed, dir, s)

	names, active := tabNames(ed)
	if len(names) != 1 || names[0] != "keep.txt" || active != 0 {
		t.Fatalf("tabs = %v (active %d), want only keep.txt active", names, active)
	}
}

// TestSessionRestoresUnsavedUntitledText checks that an untitled buffer with text is
// kept by the session and comes back as an untitled buffer with that text.
func TestSessionRestoresUnsavedUntitledText(t *testing.T) {
	isolateConfig(t)
	sessDir := t.TempDir()
	ed := newSessionEditor()
	ed.Do("new", nil)
	if res := ed.Do("insert_char", map[string]any{"text": "hello"}); res.Err != nil {
		t.Fatal(res.Err)
	}
	must(t, saveSession(ed, sessDir, ui.ExplorerState{}))

	s, _, err := session.Load(sessDir)
	must(t, err)
	restored := newSessionEditor()
	restoreSession(restored, sessDir, s)
	if doc := restored.Active(); doc == nil || doc.Buffer.Text() != "hello" {
		t.Fatalf("untitled text = %v, want hello", restored.Active())
	}
	if !restored.Active().Untitled() || !restored.Active().Modified() {
		t.Fatal("restored untitled buffer is not an unsaved untitled buffer")
	}
}

// TestSessionDropsEmptyUntitledTabs checks that an untitled buffer with no text is
// not kept, so the session does not grow an empty tab each time it is saved.
func TestSessionDropsEmptyUntitledTabs(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	must(t, os.WriteFile(file, []byte("a\n"), 0o644))
	sessDir := filepath.Join(dir, "session")
	ed := newSessionEditor()
	must(t, ed.OpenFile(file))
	ed.Do("new", nil)
	must(t, saveSession(ed, sessDir, ui.ExplorerState{}))

	s, _, err := session.Load(sessDir)
	must(t, err)
	if len(s.Files) != 1 || s.Files[0].Path != file {
		t.Fatalf("saved files = %+v, want only a.txt", s.Files)
	}
}

// TestBuildRuntimeRestoresTheSessionWhenNoFilesGiven checks the start-up path: with
// no file arguments the saved tabs open, and the saved explorer settings apply.
func TestBuildRuntimeRestoresTheSessionWhenNoFilesGiven(t *testing.T) {
	isolateConfig(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	must(t, os.WriteFile(file, []byte("a\n"), 0o644))
	sessDir := filepath.Join(dir, "session")
	ed := newSessionEditor()
	must(t, ed.OpenFile(file))
	must(t, saveSession(ed, sessDir, ui.ExplorerState{Open: true, ShowHidden: true, ShowIgnored: true}))

	rt, err := buildRuntime(t.Context(), Options{SessionDir: sessDir}, false)
	must(t, err)
	defer rt.close()
	names, _ := tabNames(rt.ed)
	if len(names) != 1 || names[0] != "a.txt" {
		t.Fatalf("tabs at start = %v, want a.txt from the session", names)
	}
	st := rt.model.ExplorerState()
	if !st.Open || !st.ShowHidden || !st.ShowIgnored {
		t.Fatalf("explorer state at start = %+v, want the saved settings", st)
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
	ed := newSessionEditor()
	must(t, ed.OpenFile(saved))
	must(t, saveSession(ed, sessDir, ui.ExplorerState{}))

	rt, err := buildRuntime(t.Context(), Options{SessionDir: sessDir, Files: []FileTarget{{Path: given}}}, false)
	must(t, err)
	defer rt.close()
	names, _ := tabNames(rt.ed)
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

// TestSessionDirDefaultsToTheUserCache checks the default session directory:
// os.UserCacheDir()/gvim/session.
func TestSessionDirDefaultsToTheUserCache(t *testing.T) {
	isolateConfig(t)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	dir, err := defaultSessionDir()
	must(t, err)
	cache, err := os.UserCacheDir()
	must(t, err)
	if want := filepath.Join(cache, "gvim", "session"); dir != want {
		t.Fatalf("defaultSessionDir() = %q, want %q", dir, want)
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
