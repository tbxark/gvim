package restore

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/session"
)

// writeMruFiles writes a.txt, b.txt and c.txt in dir.
func writeMruFiles(t *testing.T, dir string) (a, b, c string) {
	t.Helper()
	a = filepath.Join(dir, "a.txt")
	b = filepath.Join(dir, "b.txt")
	c = filepath.Join(dir, "c.txt")
	for path, text := range map[string]string{a: "a\n", b: "b\n", c: "c\n"} {
		apptest.Must(t, os.WriteFile(path, []byte(text), 0o600))
	}
	return a, b, c
}

// TestSessionPersistsMRUOrder: the editor MRU is saved as document paths (newest
// first) and restored onto the new document identities, not as tab indices.
func TestSessionPersistsMRUOrder(t *testing.T) {
	apptest.IsolateConfig(t)
	dir := t.TempDir()
	a, b, c := writeMruFiles(t, dir)
	sessDir := filepath.Join(dir, "session")

	ed := apptest.NewSessionEditor()
	apptest.Must(t, ed.OpenFile(a))
	apptest.Must(t, ed.OpenFile(b))
	apptest.Must(t, ed.OpenFile(c))
	// Activation history: C (from opening), A, B -> MRU B, A, C with B active.
	ed.ActivateTab(0)
	ed.ActivateTab(1)
	if got, want := ed.RecentTabIndices(), []int{1, 0, 2}; !slices.Equal(got, want) {
		t.Fatalf("source MRU = %v, want %v", got, want)
	}
	apptest.Must(t, SaveSession(ed, sessDir, tui.ExplorerState{}))

	s, ok, err := session.Load(sessDir)
	apptest.Must(t, err)
	if !ok {
		t.Fatal("no session was saved")
	}
	wantRefs := []session.MruFile{{Path: b}, {Path: a}, {Path: c}}
	if !slices.Equal(s.MRU, wantRefs) {
		t.Fatalf("saved MRU = %+v, want %+v", s.MRU, wantRefs)
	}

	restored := apptest.NewSessionEditor()
	RestoreSession(restored, sessDir, s)
	if got, want := restored.RecentTabIndices(), []int{1, 0, 2}; !slices.Equal(got, want) {
		t.Fatalf("restored MRU = %v, want %v", got, want)
	}
	_, active := apptest.TabNames(restored)
	if active != 1 {
		t.Fatalf("active tab = %d, want 1 (b.txt)", active)
	}
}

// TestSessionMRUSkipsMissingAndStaleReferences: a file that is gone, renamed away or
// otherwise no longer open is dropped from the saved order while the remaining
// references keep their relative order. A hand-edited/old reference cannot create a
// phantom tab or point at a different document.
func TestSessionMRUSkipsMissingAndStaleReferences(t *testing.T) {
	apptest.IsolateConfig(t)
	dir := t.TempDir()
	a, b, c := writeMruFiles(t, dir)
	sessDir := filepath.Join(dir, "session")

	ed := apptest.NewSessionEditor()
	apptest.Must(t, ed.OpenFile(a))
	apptest.Must(t, ed.OpenFile(b))
	apptest.Must(t, ed.OpenFile(c))
	apptest.Must(t, SaveSession(ed, sessDir, tui.ExplorerState{}))
	s, ok, err := session.Load(sessDir)
	apptest.Must(t, err)
	if !ok {
		t.Fatal("no session was saved")
	}
	// C is active from the open order; MRU is C, B, A.
	if want := []session.MruFile{{Path: c}, {Path: b}, {Path: a}}; !slices.Equal(s.MRU, want) {
		t.Fatalf("saved MRU = %+v, want %+v", s.MRU, want)
	}

	// B is gone. A stale reference (as a hand-edited session could hold) names a
	// renamed/closed file before the remaining entries.
	apptest.Must(t, os.Remove(b))
	s.MRU = append([]session.MruFile{{Path: filepath.Join(dir, "renamed-away.txt")}}, s.MRU...)
	restored := apptest.NewSessionEditor()
	RestoreSession(restored, sessDir, s)

	names, active := apptest.TabNames(restored)
	if len(names) != 2 || names[0] != "a.txt" || names[1] != "c.txt" || active != 1 {
		t.Fatalf("tabs = %v (active %d), want a.txt, c.txt active", names, active)
	}
	// C (tab 1) then A (tab 0); the stale and missing entries are skipped.
	if got, want := restored.RecentTabIndices(), []int{1, 0}; !slices.Equal(got, want) {
		t.Fatalf("restored MRU = %v, want %v", got, want)
	}
}

// TestSessionMRUWithoutFieldKeepsOldRestoreOrder: an older session has no MRU field;
// restore still opens the files in tab order and derives the history as before.
func TestSessionMRUWithoutFieldKeepsOldRestoreOrder(t *testing.T) {
	apptest.IsolateConfig(t)
	dir := t.TempDir()
	a, b, c := writeMruFiles(t, dir)
	s := session.Session{
		Files:  []session.FileState{{Path: a}, {Path: b}, {Path: c}},
		Active: 1,
	}
	restored := apptest.NewSessionEditor()
	RestoreSession(restored, dir, s)
	_, active := apptest.TabNames(restored)
	if active != 1 {
		t.Fatalf("active tab = %d, want 1", active)
	}
	if s.MRU != nil {
		t.Fatalf("test session unexpectedly has MRU %+v", s.MRU)
	}
	if got := restored.RecentTabIndices(); len(got) != 3 || got[0] != 1 {
		t.Fatalf("restored MRU = %v, want the active tab first and three tabs", got)
	}
}

// TestSessionMRURestoresUntitledBuffer: a pathless buffer is saved and matched by its
// label, so an unsaved untitled tab keeps its MRU rank across a restart.
func TestSessionMRURestoresUntitledBuffer(t *testing.T) {
	apptest.IsolateConfig(t)
	dir := t.TempDir()
	a, _, _ := writeMruFiles(t, dir)
	sessDir := filepath.Join(dir, "session")

	ed := apptest.NewSessionEditor()
	apptest.Must(t, ed.OpenFile(a))
	ed.Do("new", nil)
	if r := ed.Do("insert_char", map[string]any{"text": "unsaved"}); r.Err != nil {
		t.Fatal(r.Err)
	}
	// A (tab 0) was activated, then the untitled tab (tab 1): MRU is untitled, A.
	ed.ActivateTab(0)
	ed.ActivateTab(1)
	apptest.Must(t, SaveSession(ed, sessDir, tui.ExplorerState{}))

	s, ok, err := session.Load(sessDir)
	apptest.Must(t, err)
	if !ok {
		t.Fatal("no session was saved")
	}
	if len(s.MRU) != 2 || s.MRU[0].Path != "" || s.MRU[1].Path != a {
		t.Fatalf("saved MRU = %+v, want the untitled buffer then a.txt", s.MRU)
	}
	restored := apptest.NewSessionEditor()
	RestoreSession(restored, sessDir, s)
	_, active := apptest.TabNames(restored)
	if active != 1 {
		t.Fatalf("active tab = %d, want the restored untitled tab", active)
	}
	if got, want := restored.RecentTabIndices(), []int{1, 0}; !slices.Equal(got, want) {
		t.Fatalf("restored MRU = %v, want %v", got, want)
	}
}
