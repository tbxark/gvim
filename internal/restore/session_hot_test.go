package restore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/session"
)

// TestRestoreHotExitOpensOnlyTheUnsavedFiles checks the restore of
// editor.restore_previous_session=false: the saved clean tabs stay closed and
// only the files with stashed unsaved text (and unnamed buffers) come back, with
// the stashed text.
func TestRestoreHotExitOpensOnlyTheUnsavedFiles(t *testing.T) {
	apptest.IsolateConfig(t)
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "session")
	clean := filepath.Join(dir, "clean.txt")
	edited := filepath.Join(dir, "edited.txt")
	for path, body := range map[string]string{clean: "clean\n", edited: "on disk\n"} {
		apptest.Must(t, os.WriteFile(path, []byte(body), 0o644))
	}
	stash, err := session.StashContent(sessDir, "edited unsaved\n")
	apptest.Must(t, err)
	untitledStash, err := session.StashContent(sessDir, "scratch\n")
	apptest.Must(t, err)
	s := session.Session{
		Explorer: true,
		Active:   0,
		Files: []session.FileState{
			{Path: clean, Line: 1, Col: 1},
			{Path: edited, StashFile: stash, Line: 1, Col: 1},
			{Path: "", StashFile: untitledStash},
		},
	}

	ed := apptest.NewSessionEditor()
	if failed := RestoreHotExit(ed, sessDir, s); len(failed) != 0 {
		t.Fatalf("RestoreHotExit failed = %v", failed)
	}
	tabs := ed.Tabs()
	if len(tabs) != 2 || tabs[0].Path != edited || tabs[1].Path != "" {
		t.Fatalf("tabs = %+v, want the edited file and the unnamed buffer", tabs)
	}
	ed.ActivateTab(0)
	if got := ed.Active().Buffer.Text(); got != "edited unsaved\n" || !ed.Active().Modified() {
		t.Fatalf("edited text = %q (modified %v), want the stashed text, modified", got, ed.Active().Modified())
	}
	ed.ActivateTab(1)
	if got := ed.Active().Buffer.Text(); got != "scratch\n" || !ed.Active().Modified() {
		t.Fatalf("unnamed text = %q (modified %v), want the stashed text, modified", got, ed.Active().Modified())
	}
	ed.ActivateTab(0)
	if c := ed.Active().Cursor; c.Line != 0 || c.Col != 0 {
		t.Fatalf("cursor of the edited file = %+v, want its start", c)
	}
}

// TestRestoreHotExitWithNothingUnsavedOpensNoTabs checks the empty case: a
// session whose saved tabs are all clean has no hot-exit content, so the restore
// leaves the editor as it is.
func TestRestoreHotExitWithNothingUnsavedOpensNoTabs(t *testing.T) {
	apptest.IsolateConfig(t)
	dir := t.TempDir()
	clean := filepath.Join(dir, "clean.txt")
	apptest.Must(t, os.WriteFile(clean, []byte("clean\n"), 0o644))
	ed := apptest.NewSessionEditor()
	if failed := RestoreHotExit(ed, filepath.Join(dir, "session"), session.Session{
		Files: []session.FileState{{Path: clean}},
	}); len(failed) != 0 {
		t.Fatalf("RestoreHotExit failed = %v", failed)
	}
	if names, _ := apptest.TabNames(ed); len(names) != 0 {
		t.Fatalf("tabs = %v, want none", names)
	}
}
