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

func TestLargeSessionRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ed, path := apptest.LargeSessionFile(t, dir)
	d := ed.Active()
	want := d.Buffer.Text()
	cursor := d.Cursor
	sess := filepath.Join(dir, "session")
	apptest.Must(t, WriteSession(ed, sess, tui.ExplorerState{}, nil))
	s, _, err := session.Load(sess)
	apptest.Must(t, err)
	if len(s.Files) != 1 || s.Files[0].OverlayFile == "" {
		t.Fatalf("session = %+v, want the overlay stash of the large file", s)
	}
	st, err := session.ReadOverlay(sess, s.Files[0].OverlayFile)
	apptest.Must(t, err)
	if st.Path != path || st.Size != d.Buffer.Stats().TotalBytes {
		t.Errorf("stash identity = %+v", st)
	}

	// A new session opens the file and puts the edits back.
	ed2 := editor.New(apptest.LargeOpts())
	if failed := RestoreSession(ed2, sess, s); len(failed) != 0 {
		t.Fatalf("restoreSession failed: %+v", failed)
	}
	d2 := ed2.Active()
	if d2 == nil || !d2.Large() || !d2.Modified() {
		t.Fatalf("restored document: %v, want a modified large document", d2)
	}
	if got := d2.Buffer.Text(); got != want {
		t.Errorf("restored text differs:\n got %q\nwant %q", got, want)
	}
	if d2.Cursor != cursor {
		t.Errorf("cursor = %+v, want %+v", d2.Cursor, cursor)
	}
	// Undo goes back to the file as it is on disk.
	ed2.Do("undo", nil)
	disk, err := os.ReadFile(path)
	apptest.Must(t, err)
	if got := d2.Buffer.Text(); got != string(disk) {
		t.Errorf("undo of the restore did not give the file back")
	}
}

func TestLargeSessionKeepsAStashForAFileThatChanged(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ed, path := apptest.LargeSessionFile(t, dir)
	sess := filepath.Join(dir, "session")
	apptest.Must(t, WriteSession(ed, sess, tui.ExplorerState{}, nil))
	s, _, err := session.Load(sess)
	apptest.Must(t, err)
	stash := s.Files[0].OverlayFile
	disk, err := os.ReadFile(path)
	apptest.Must(t, err)
	apptest.Must(t, os.WriteFile(path, append(disk, "appended by someone else\n"...), 0o644))

	ed2 := editor.New(apptest.LargeOpts())
	failed := RestoreSession(ed2, sess, s)
	if len(failed) != 1 || failed[0].File.OverlayFile != stash {
		t.Fatalf("failed = %+v, want the overlay stash kept as unrestored", failed)
	}
	if d := ed2.Active(); d == nil || d.Modified() || d.Buffer.Line(3) == "" {
		t.Fatalf("the stash of another version of the file changed the document")
	}
	if n := RestoreNotice(failed); !strings.Contains(n, "big.txt") || !strings.Contains(n, "changed") {
		t.Errorf("notice = %q, want the file and the reason", n)
	}
	if n := RestoreNotice(failed); !strings.Contains(n, filepath.Join(sess, stash)) {
		t.Errorf("notice = %q, want the path of the stash file", n)
	}
	// Saving the session again keeps the stash file that could not be restored.
	apptest.Must(t, WriteSession(ed2, sess, tui.ExplorerState{}, keptFiles(failed)))
	if _, err := os.Stat(filepath.Join(sess, stash)); err != nil {
		t.Errorf("the stash that could not be restored was removed: %v", err)
	}
}

func TestLargeSessionKeepsAStashForAFileThatIsGoneOrSmallNow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(t *testing.T, path string)
		opts   editor.Options
	}{
		{"deleted", func(t *testing.T, path string) { apptest.Must(t, os.Remove(path)) }, apptest.LargeOpts()},
		{"threshold raised", func(*testing.T, string) {}, editor.Options{Empty: true, LargeFileMode: true, LargeFileThresholdBytes: 1 << 30}},
		{"large file mode off", func(*testing.T, string) {}, editor.Options{Empty: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			ed, path := apptest.LargeSessionFile(t, dir)
			sess := filepath.Join(dir, "session")
			apptest.Must(t, WriteSession(ed, sess, tui.ExplorerState{}, nil))
			s, _, err := session.Load(sess)
			apptest.Must(t, err)
			tc.change(t, path)
			ed2 := editor.New(tc.opts)
			failed := RestoreSession(ed2, sess, s)
			if len(failed) != 1 || failed[0].File.OverlayFile != s.Files[0].OverlayFile {
				t.Fatalf("failed = %+v, want the overlay stash kept", failed)
			}
			if d := ed2.Active(); d != nil && d.Modified() {
				t.Errorf("a stash that does not fit changed the document")
			}
		})
	}
}

func TestLargeRecoveryCopyRestoresAfterACrash(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ed, _ := apptest.LargeSessionFile(t, dir)
	want := ed.Active().Buffer.Text()
	rec := filepath.Join(dir, "recovery")
	w := NewRecoveryWriter(ed, rec)
	apptest.Must(t, w.Plan()())
	files, err := filepath.Glob(filepath.Join(rec, "overlay-*.json"))
	apptest.Must(t, err)
	if len(files) != 1 {
		t.Fatalf("%d overlay copies, want 1", len(files))
	}
	// Nothing changed: the next write keeps the copy and writes no second one.
	apptest.Must(t, w.Plan()())
	if again, _ := filepath.Glob(filepath.Join(rec, "overlay-*.json")); len(again) != 1 {
		t.Errorf("%d overlay copies after a quiet write, want 1", len(again))
	}

	ed2 := editor.New(apptest.LargeOpts())
	failed, n := recoverCrash(ed2, rec)
	if len(failed) != 0 || n != 1 {
		t.Fatalf("recoverCrash = %+v, %d", failed, n)
	}
	d := ed2.Active()
	if d == nil || !d.Modified() || d.Buffer.Text() != want {
		t.Fatalf("the recovered document does not hold the unsaved edits")
	}
}

func TestSessionWithoutEditsKeepsNoOverlay(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	apptest.Must(t, os.WriteFile(path, []byte(strings.Repeat("some line of text\n", 250)), 0o644))
	ed := editor.New(apptest.LargeOpts())
	apptest.Must(t, ed.OpenFile(path))
	sess := filepath.Join(dir, "session")
	apptest.Must(t, WriteSession(ed, sess, tui.ExplorerState{}, nil))
	s, _, err := session.Load(sess)
	apptest.Must(t, err)
	if len(s.Files) != 1 || s.Files[0].OverlayFile != "" || s.Files[0].StashFile != "" {
		t.Fatalf("session = %+v, want the file listed with no stash", s)
	}
}

func TestCrashRecoveryOverAnAlreadyRestoredLargeDocument(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ed, _ := apptest.LargeSessionFile(t, dir)
	sess, rec := filepath.Join(dir, "session"), filepath.Join(dir, "recovery")
	apptest.Must(t, WriteSession(ed, sess, tui.ExplorerState{}, nil))
	s, _, err := session.Load(sess)
	apptest.Must(t, err)

	// The run that restored the session made newer edits, then crashed.
	ed2 := editor.New(apptest.LargeOpts())
	if failed := RestoreSession(ed2, sess, s); len(failed) != 0 {
		t.Fatalf("restoreSession failed: %+v", failed)
	}
	apptest.IndexComplete(t, ed2)
	apptest.Must(t, ed2.GotoLine(100, 1))
	ed2.Do("insert_char", map[string]any{"text": "NEWER "})
	want := ed2.Active().Buffer.Text()
	apptest.Must(t, NewRecoveryWriter(ed2, rec).Plan()())

	// The next start restores the session, and recovery must bring the newer edits.
	ed3 := editor.New(apptest.LargeOpts())
	if failed := RestoreSession(ed3, sess, s); len(failed) != 0 {
		t.Fatalf("restoreSession failed: %+v", failed)
	}
	failed, n := recoverCrash(ed3, rec)
	if len(failed) != 0 || n != 1 {
		t.Fatalf("recoverCrash = %+v, %d: want the newer edits restored", failed, n)
	}
	apptest.IndexComplete(t, ed3)
	if tabs := ed3.Tabs(); len(tabs) != 1 {
		t.Fatalf("%d tabs, want 1", len(tabs))
	}
	if d := ed3.Active(); d.Buffer.Text() != want || !d.Modified() {
		t.Errorf("recovered text lacks the newer edit: line 99 = %q", d.Buffer.Line(99))
	}
}

func TestSessionKeepsBothTheLiveEditsAndAStashThatFailedToRestore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// The file is longer than the part of it that the editor indexes at once, so the
	// restore waits for the index and fails when the index is complete.
	path := filepath.Join(dir, "big.txt")
	apptest.Must(t, os.WriteFile(path, []byte(strings.Repeat("x\n", 2_600_000)), 0o644))
	ed := editor.New(apptest.LargeOpts())
	apptest.Must(t, ed.OpenFile(path))
	ed.Do("insert_char", map[string]any{"text": "edit "})
	sess := filepath.Join(dir, "session")
	apptest.Must(t, WriteSession(ed, sess, tui.ExplorerState{}, nil))
	s, _, err := session.Load(sess)
	apptest.Must(t, err)
	st, err := session.ReadOverlay(sess, s.Files[0].OverlayFile)
	apptest.Must(t, err)
	// The identity fits the file, but the overlay claims more lines than it has.
	st.Overlay.TailFirst = 50_000_000
	bad, err := session.StashOverlay(sess, st)
	apptest.Must(t, err)
	s.Files[0].OverlayFile = bad

	ed2 := editor.New(apptest.LargeOpts())
	if failed := RestoreSession(ed2, sess, s); len(failed) != 0 {
		t.Fatalf("restoreSession failed: %+v", failed)
	}
	apptest.IndexComplete(t, ed2)
	d := ed2.Active()
	if d.Buffer.Modified() || !d.Modified() {
		t.Fatalf("buffer modified %v, document modified %v: want a failed restore that is still unsaved", d.Buffer.Modified(), d.Modified())
	}
	apptest.Must(t, ed2.GotoLine(1, 1))
	ed2.Do("insert_char", map[string]any{"text": "NEWEDIT "})

	apptest.Must(t, WriteSession(ed2, sess, tui.ExplorerState{}, nil))
	s2, _, err := session.Load(sess)
	apptest.Must(t, err)
	if len(s2.Files) != 2 || s2.Files[0].Path != path || s2.Files[1].Path != path {
		t.Fatalf("session files = %+v, want the live edits and the failed stash", s2.Files)
	}
	live, err := session.ReadOverlay(sess, s2.Files[0].OverlayFile)
	apptest.Must(t, err)
	kept, err := session.ReadOverlay(sess, s2.Files[1].OverlayFile)
	apptest.Must(t, err)
	if live.Overlay.TailFirst == 50_000_000 || kept.Overlay.TailFirst != 50_000_000 {
		t.Errorf("tail of the live edits %d, of the failed stash %d", live.Overlay.TailFirst, kept.Overlay.TailFirst)
	}

	// The next start restores the live edits, and the failed stash is reported and kept.
	ed3 := editor.New(apptest.LargeOpts())
	failed := RestoreSession(ed3, sess, s2)
	if len(failed) != 1 || failed[0].File.OverlayFile != s2.Files[1].OverlayFile {
		t.Fatalf("failed = %+v, want the failed stash kept", failed)
	}
	apptest.IndexComplete(t, ed3)
	if d := ed3.Active(); len(ed3.Tabs()) != 1 || !strings.HasPrefix(d.Buffer.Line(0), "NEWEDIT ") {
		t.Errorf("tabs %d: the live edits did not come back", len(ed3.Tabs()))
	}
}

// keptFiles returns the files of failed, in order, for the session save to list.
func keptFiles(failed []Unrestored) []session.FileState {
	files := make([]session.FileState, len(failed))
	for i, u := range failed {
		files[i] = u.File
	}
	return files
}
