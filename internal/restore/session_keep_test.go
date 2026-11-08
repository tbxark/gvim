package restore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/session"
)

// saveWithFailedRestore writes a session whose one file has unsaved text in a stash
// and cannot be opened: its path is a directory. It restores the session into a new
// editor, and then saves it again, carrying the failed restores when carry is set. It
// returns the session directory, the stash name and the restore failures.
func saveWithFailedRestore(t *testing.T, carry bool) (sessDir, stash string, failed []Unrestored) {
	t.Helper()
	dir := t.TempDir()
	sessDir = filepath.Join(dir, "session")
	stash, err := session.StashContent(sessDir, "unsaved edits")
	apptest.Must(t, err)
	blocked := filepath.Join(dir, "blocked.go")
	apptest.Must(t, os.Mkdir(blocked, 0o755))
	apptest.Must(t, session.Save(sessDir, session.Session{Files: []session.FileState{{Path: blocked, StashFile: stash}}}))
	s, ok, err := session.Load(sessDir)
	apptest.Must(t, err)
	if !ok {
		t.Fatal("the session was not saved")
	}
	ed := editor.New(editor.Options{})
	failed = RestoreSession(ed, sessDir, s)
	var carried []session.FileState
	if carry {
		for _, u := range failed {
			carried = append(carried, u.File)
		}
	}
	apptest.Must(t, WriteSession(ed, sessDir, tui.ExplorerState{}, carried))
	return sessDir, stash, failed
}

// TestRestoreFailureKeepsTheStashThroughTheNextSave checks that the unsaved text of a
// file that could not be restored survives the next session save, and that the save
// removes the stash when nothing carries it. The second case is the control that
// shows the first one is not passing by accident.
func TestRestoreFailureKeepsTheStashThroughTheNextSave(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		carry     bool
		wantStash bool
	}{
		{name: "the failed restore is carried into the save", carry: true, wantStash: true},
		{name: "nothing carries the failed restore", carry: false, wantStash: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sessDir, stash, failed := saveWithFailedRestore(t, tt.carry)
			if len(failed) != 1 {
				t.Fatalf("restore failures = %d, want 1", len(failed))
			}
			_, err := os.Stat(filepath.Join(sessDir, stash))
			if got := err == nil; got != tt.wantStash {
				t.Fatalf("stash exists after the save = %v, want %v (stat error %v)", got, tt.wantStash, err)
			}
		})
	}
}

// TestRestoreFailureOpensNoTab checks that a file whose stash cannot be read is not
// opened in a tab, so the session save does not record a tab without its text.
func TestRestoreFailureOpensNoTab(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "session")
	apptest.Must(t, session.Save(sessDir, session.Session{Files: []session.FileState{
		{Path: filepath.Join(dir, "a.go"), StashFile: "stash-missing.txt"},
	}}))
	s, _, err := session.Load(sessDir)
	apptest.Must(t, err)
	apptest.Must(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("disk\n"), 0o644))
	ed := editor.New(editor.Options{})
	before := len(ed.Tabs())
	failed := RestoreSession(ed, sessDir, s)
	if len(failed) != 1 {
		t.Fatalf("restore failures = %d, want 1", len(failed))
	}
	if n := len(ed.Tabs()); n != before {
		t.Fatalf("tabs after a failed restore = %d, want %d (no tab opened)", n, before)
	}
}

// TestRestoreNoticeNamesEveryFailedFile checks the notice that tells the user which
// files keep their unsaved text. No failure gives no notice.
func TestRestoreNoticeNamesEveryFailedFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		failed []Unrestored
		want   []string
	}{
		{name: "no failure", failed: nil, want: nil},
		{
			name:   "one file",
			failed: []Unrestored{{File: session.FileState{Path: "/w/blocked.go"}}},
			want:   []string{"blocked.go", "kept"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := RestoreNotice(tt.failed)
			if tt.want == nil {
				if got != "" {
					t.Fatalf("notice = %q, want none", got)
				}
				return
			}
			for _, sub := range tt.want {
				if !strings.Contains(got, sub) {
					t.Fatalf("notice = %q, want it to contain %q", got, sub)
				}
			}
		})
	}
}
