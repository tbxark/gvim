package restore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/session"
)

func TestSessionRoundTripKeepsPageView(t *testing.T) {
	apptest.IsolateConfig(t)
	dir := t.TempDir()
	md := filepath.Join(dir, "notes.md")
	apptest.Must(t, os.WriteFile(md, []byte("# Title\ntext\n"), 0o644))
	sessDir := filepath.Join(dir, "session")

	ed := apptest.NewSessionEditor()
	apptest.Must(t, ed.OpenFile(md))
	on := true
	view := ed.Active().View()
	view.PageView = &on
	view.PageWidth = 64
	ed.Active().SetView(*view)
	apptest.Must(t, SaveSession(ed, sessDir, tui.ExplorerState{}))

	restored := apptest.NewSessionEditor()
	s, ok, err := session.Load(sessDir)
	apptest.Must(t, err)
	if !ok {
		t.Fatal("no session was saved")
	}
	RestoreSession(restored, sessDir, s)
	got := restored.Active().View()
	if got.PageView == nil || !*got.PageView {
		t.Fatalf("restored page view = %v, want on", got.PageView)
	}
	if got.PageWidth != 64 {
		t.Fatalf("restored page width = %d, want 64", got.PageWidth)
	}
}

func TestSessionPageViewFieldsAreOptional(t *testing.T) {
	apptest.IsolateConfig(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "plain.txt")
	apptest.Must(t, os.WriteFile(p, []byte("plain\n"), 0o644))
	ed := apptest.NewSessionEditor()
	apptest.Must(t, ed.OpenFile(p))
	sessDir := filepath.Join(dir, "session")
	apptest.Must(t, SaveSession(ed, sessDir, tui.ExplorerState{}))
	s, ok, err := session.Load(sessDir)
	apptest.Must(t, err)
	if !ok || len(s.Files) != 1 {
		t.Fatalf("session = %+v", s)
	}
	if s.Files[0].PageView != nil || s.Files[0].PageWidth != 0 {
		t.Fatalf("plain file page fields = %v/%d, want none", s.Files[0].PageView, s.Files[0].PageWidth)
	}
}
