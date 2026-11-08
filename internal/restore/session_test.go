package restore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/session"
)

func TestSessionRoundTripKeepsTabsCursorsActiveAndUnsavedText(t *testing.T) {
	apptest.IsolateConfig(t)
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	apptest.Must(t, os.WriteFile(a, []byte("one\ntwo\nthree\n"), 0o644))
	apptest.Must(t, os.WriteFile(b, []byte("bee\n"), 0o644))
	sessDir := filepath.Join(dir, "session")

	ed := apptest.NewSessionEditor()
	apptest.Must(t, ed.OpenFile(a))
	apptest.Must(t, ed.GotoLine(2, 2))
	apptest.Must(t, ed.OpenFile(b))
	if res := ed.Do("insert_char", map[string]any{"text": "Z"}); res.Err != nil {
		t.Fatal(res.Err)
	}
	apptest.Must(t, SaveSession(ed, sessDir, tui.ExplorerState{Open: true, ShowHidden: true}))

	restored := apptest.NewSessionEditor()
	s, ok, err := session.Load(sessDir)
	apptest.Must(t, err)
	if !ok {
		t.Fatal("no session was saved")
	}
	RestoreSession(restored, sessDir, s)

	names, active := apptest.TabNames(restored)
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

func TestSessionSkipsSavedFilesThatAreGone(t *testing.T) {
	apptest.IsolateConfig(t)
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.txt")
	apptest.Must(t, os.WriteFile(keep, []byte("k\n"), 0o644))
	s := session.Session{Files: []session.FileState{
		{Path: filepath.Join(dir, "gone.txt"), Line: 1, Col: 1},
		{Path: keep, Line: 1, Col: 1},
	}, Active: 1}
	ed := apptest.NewSessionEditor()
	RestoreSession(ed, dir, s)

	names, active := apptest.TabNames(ed)
	if len(names) != 1 || names[0] != "keep.txt" || active != 0 {
		t.Fatalf("tabs = %v (active %d), want only keep.txt active", names, active)
	}
}

func TestSessionRestoresUnsavedUntitledText(t *testing.T) {
	apptest.IsolateConfig(t)
	sessDir := t.TempDir()
	ed := apptest.NewSessionEditor()
	ed.Do("new", nil)
	if res := ed.Do("insert_char", map[string]any{"text": "hello"}); res.Err != nil {
		t.Fatal(res.Err)
	}
	apptest.Must(t, SaveSession(ed, sessDir, tui.ExplorerState{}))

	s, _, err := session.Load(sessDir)
	apptest.Must(t, err)
	restored := apptest.NewSessionEditor()
	RestoreSession(restored, sessDir, s)
	if doc := restored.Active(); doc == nil || doc.Buffer.Text() != "hello" {
		t.Fatalf("untitled text = %v, want hello", restored.Active())
	}
	if !restored.Active().Untitled() || !restored.Active().Modified() {
		t.Fatal("restored untitled buffer is not an unsaved untitled buffer")
	}
}

func TestSessionDropsEmptyUntitledTabs(t *testing.T) {
	apptest.IsolateConfig(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	apptest.Must(t, os.WriteFile(file, []byte("a\n"), 0o644))
	sessDir := filepath.Join(dir, "session")
	ed := apptest.NewSessionEditor()
	apptest.Must(t, ed.OpenFile(file))
	ed.Do("new", nil)
	apptest.Must(t, SaveSession(ed, sessDir, tui.ExplorerState{}))

	s, _, err := session.Load(sessDir)
	apptest.Must(t, err)
	if len(s.Files) != 1 || s.Files[0].Path != file {
		t.Fatalf("saved files = %+v, want only a.txt", s.Files)
	}
}

func TestSessionDirDefaultsToTheUserCache(t *testing.T) {
	apptest.IsolateConfig(t)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	dir, err := DefaultSessionDir()
	apptest.Must(t, err)
	cache, err := os.UserCacheDir()
	apptest.Must(t, err)
	if want := filepath.Join(cache, "gvim", "session"); dir != want {
		t.Fatalf("DefaultSessionDir() = %q, want %q", dir, want)
	}
}
