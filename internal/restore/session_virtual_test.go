package restore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/session"
)

// TestSessionRoundTripKeepsTheVirtualCaretPosition checks R4-4 persistence: a caret
// parked on empty rows below the last line is saved with the file state and parked
// there again by the restore.
func TestSessionRoundTripKeepsTheVirtualCaretPosition(t *testing.T) {
	apptest.IsolateConfig(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	apptest.Must(t, os.WriteFile(path, []byte("one\ntwo\n"), 0o644))
	sessDir := filepath.Join(dir, "session")

	ed := apptest.NewSessionEditor()
	apptest.Must(t, ed.OpenFile(path))
	if got := ed.Active().SetVirtualLines(4, 9); got != 4 {
		t.Fatalf("SetVirtualLines took %d rows, want 4", got)
	}
	apptest.Must(t, SaveSession(ed, sessDir, tui.ExplorerState{}))

	s, ok, err := session.Load(sessDir)
	apptest.Must(t, err)
	if !ok || len(s.Files) != 1 {
		t.Fatalf("session = %+v, %v", s, ok)
	}
	if got := s.Files[0]; got.VirtualLines != 4 || got.VirtualColumn != 9 {
		t.Fatalf("saved file state = %+v, want the virtual caret at 4 rows, column 9", got)
	}

	restored := apptest.NewSessionEditor()
	RestoreSession(restored, sessDir, s)
	doc := restored.Active()
	if doc == nil {
		t.Fatal("no tab was restored")
	}
	if got := doc.VirtualLines(); got != 4 {
		t.Fatalf("restored VirtualLines = %d, want 4", got)
	}
	if got := doc.VirtualLineColumn(); got != 9 {
		t.Fatalf("restored VirtualLineColumn = %d, want 9", got)
	}
}

// TestSessionRestoreOfAnOldFileStateKeepsTheCaretOnText checks the migration: a file
// state without the virtual fields leaves the caret on text.
func TestSessionRestoreOfAnOldFileStateKeepsTheCaretOnText(t *testing.T) {
	apptest.IsolateConfig(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	apptest.Must(t, os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644))
	s := session.Session{Files: []session.FileState{{Path: path, Line: 2, Col: 2}}}

	ed := apptest.NewSessionEditor()
	RestoreSession(ed, dir, s)
	doc := ed.Active()
	if doc == nil {
		t.Fatal("no tab was restored")
	}
	if doc.VirtualLines() != 0 {
		t.Fatalf("restored VirtualLines = %d, want the caret on text", doc.VirtualLines())
	}
	if doc.Cursor.Line != 1 || doc.Cursor.Col != 1 {
		t.Fatalf("restored cursor = %+v, want line 2 column 2 (zero-based 1,1)", doc.Cursor)
	}
}
