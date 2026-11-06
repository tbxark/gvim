package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/keymap"
	"github.com/tbxark/gvim/internal/session"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui"
)

// largeOpts are the editor options of the session tests: a 1 KiB threshold makes a
// file of a few KiB large.
func largeOpts() editor.Options {
	return editor.Options{Empty: true, LargeFileMode: true, LargeFileThresholdBytes: 1024}
}

// largeSessionFile writes a file of 250 lines, opens it in a large-mode editor, makes
// two edits far apart, and returns the editor and the path.
func largeSessionFile(t *testing.T, dir string) (*editor.Editor, string) {
	t.Helper()
	var sb strings.Builder
	for i := range 250 {
		sb.WriteString("line ")
		sb.WriteString(strings.Repeat("x", i%5))
		sb.WriteString("\n")
	}
	path := filepath.Join(dir, "big.txt")
	must(t, os.WriteFile(path, []byte(sb.String()), 0o644))
	ed := editor.New(largeOpts())
	must(t, ed.OpenFile(path))
	d := ed.Active()
	if !d.Large() {
		t.Fatal("the document is not large")
	}
	for !d.Index().Complete {
		job, ok := ed.PlanIndex()
		if !ok {
			t.Fatal("no index job for an incomplete index")
		}
		ed.ApplyIndex(job.Run(t.Context()))
	}
	must(t, ed.GotoLine(200, 1))
	ed.Do("insert_char", map[string]any{"text": "deep "})
	must(t, ed.GotoLine(3, 3))
	ed.Do("insert_char", map[string]any{"text": "ABC"})
	ed.Do("insert_newline", nil)
	return ed, path
}

func TestLargeSessionRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ed, path := largeSessionFile(t, dir)
	d := ed.Active()
	want := d.Buffer.Text()
	cursor := d.Cursor
	sess := filepath.Join(dir, "session")
	must(t, writeSession(ed, sess, ui.ExplorerState{}, nil))
	s, _, err := session.Load(sess)
	must(t, err)
	if len(s.Files) != 1 || s.Files[0].OverlayFile == "" {
		t.Fatalf("session = %+v, want the overlay stash of the large file", s)
	}
	st, err := session.ReadOverlay(sess, s.Files[0].OverlayFile)
	must(t, err)
	if st.Path != path || st.Size != d.Buffer.Stats().TotalBytes {
		t.Errorf("stash identity = %+v", st)
	}

	// A new session opens the file and puts the edits back.
	ed2 := editor.New(largeOpts())
	if failed := restoreSession(ed2, sess, s); len(failed) != 0 {
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
	must(t, err)
	if got := d2.Buffer.Text(); got != string(disk) {
		t.Errorf("undo of the restore did not give the file back")
	}
}

func TestLargeSessionKeepsAStashForAFileThatChanged(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ed, path := largeSessionFile(t, dir)
	sess := filepath.Join(dir, "session")
	must(t, writeSession(ed, sess, ui.ExplorerState{}, nil))
	s, _, err := session.Load(sess)
	must(t, err)
	stash := s.Files[0].OverlayFile
	disk, err := os.ReadFile(path)
	must(t, err)
	must(t, os.WriteFile(path, append(disk, "appended by someone else\n"...), 0o644))

	ed2 := editor.New(largeOpts())
	failed := restoreSession(ed2, sess, s)
	if len(failed) != 1 || failed[0].file.OverlayFile != stash {
		t.Fatalf("failed = %+v, want the overlay stash kept as unrestored", failed)
	}
	if d := ed2.Active(); d == nil || d.Modified() || d.Buffer.Line(3) == "" {
		t.Fatalf("the stash of another version of the file changed the document")
	}
	if n := restoreNotice(failed); !strings.Contains(n, "big.txt") || !strings.Contains(n, "changed") {
		t.Errorf("notice = %q, want the file and the reason", n)
	}
	if n := restoreNotice(failed); !strings.Contains(n, filepath.Join(sess, stash)) {
		t.Errorf("notice = %q, want the path of the stash file", n)
	}
	// Saving the session again keeps the stash file that could not be restored.
	must(t, writeSession(ed2, sess, ui.ExplorerState{}, keptFiles(failed)))
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
		{"deleted", func(t *testing.T, path string) { must(t, os.Remove(path)) }, largeOpts()},
		{"threshold raised", func(*testing.T, string) {}, editor.Options{Empty: true, LargeFileMode: true, LargeFileThresholdBytes: 1 << 30}},
		{"large file mode off", func(*testing.T, string) {}, editor.Options{Empty: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			ed, path := largeSessionFile(t, dir)
			sess := filepath.Join(dir, "session")
			must(t, writeSession(ed, sess, ui.ExplorerState{}, nil))
			s, _, err := session.Load(sess)
			must(t, err)
			tc.change(t, path)
			ed2 := editor.New(tc.opts)
			failed := restoreSession(ed2, sess, s)
			if len(failed) != 1 || failed[0].file.OverlayFile != s.Files[0].OverlayFile {
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
	ed, _ := largeSessionFile(t, dir)
	want := ed.Active().Buffer.Text()
	rec := filepath.Join(dir, "recovery")
	w := newRecoveryWriter(ed, rec)
	must(t, w.Plan()())
	files, err := filepath.Glob(filepath.Join(rec, "overlay-*.json"))
	must(t, err)
	if len(files) != 1 {
		t.Fatalf("%d overlay copies, want 1", len(files))
	}
	// Nothing changed: the next write keeps the copy and writes no second one.
	must(t, w.Plan()())
	if again, _ := filepath.Glob(filepath.Join(rec, "overlay-*.json")); len(again) != 1 {
		t.Errorf("%d overlay copies after a quiet write, want 1", len(again))
	}

	ed2 := editor.New(largeOpts())
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
	must(t, os.WriteFile(path, []byte(strings.Repeat("some line of text\n", 250)), 0o644))
	ed := editor.New(largeOpts())
	must(t, ed.OpenFile(path))
	sess := filepath.Join(dir, "session")
	must(t, writeSession(ed, sess, ui.ExplorerState{}, nil))
	s, _, err := session.Load(sess)
	must(t, err)
	if len(s.Files) != 1 || s.Files[0].OverlayFile != "" || s.Files[0].StashFile != "" {
		t.Fatalf("session = %+v, want the file listed with no stash", s)
	}
}

// quitModel is a UI model over ed, which a runtime needs to save the session.
func quitModel(ed *editor.Editor) *ui.Model {
	return ui.New(newUIDeps(ed, keymap.Default(), theme.Theme{}, nil, nil))
}

func TestStashForQuit(t *testing.T) {
	t.Parallel()
	t.Run("a saved session keeps the stash in the session", func(t *testing.T) {
		dir := t.TempDir()
		ed, _ := largeSessionFile(t, dir)
		sess := filepath.Join(dir, "session")
		rt := &runtime{ed: ed, model: quitModel(ed), sessionDir: sess, stashDir: sess}
		must(t, rt.stashForQuit())
		s, ok, err := session.Load(sess)
		must(t, err)
		if !ok || len(s.Files) != 1 || s.Files[0].OverlayFile == "" {
			t.Fatalf("session = %+v (found %v), want the overlay stash", s, ok)
		}
	})
	t.Run("without a saved session each stash is a file of its own", func(t *testing.T) {
		dir := t.TempDir()
		ed, _ := largeSessionFile(t, dir)
		sess := filepath.Join(dir, "session")
		rt := &runtime{ed: ed, stashDir: sess}
		must(t, rt.stashForQuit())
		files, err := filepath.Glob(filepath.Join(sess, "overlay-*.json"))
		must(t, err)
		if len(files) != 1 {
			t.Fatalf("%d overlay stashes, want 1", len(files))
		}
		if _, err := os.Stat(filepath.Join(sess, "session.json")); err == nil {
			t.Errorf("the stash replaced the saved session")
		}
	})
	t.Run("a stash that cannot be written stops the quit", func(t *testing.T) {
		dir := t.TempDir()
		ed, _ := largeSessionFile(t, dir)
		blocker := filepath.Join(dir, "file")
		must(t, os.WriteFile(blocker, []byte("x"), 0o644))
		runtimes := map[string]*runtime{
			"session dir under a file": {ed: ed, model: quitModel(ed), sessionDir: filepath.Join(blocker, "s"), stashDir: filepath.Join(blocker, "s")},
			"stash dir under a file":   {ed: ed, stashDir: filepath.Join(blocker, "s")},
			"no directory":             {ed: ed},
		}
		for name, rt := range runtimes {
			if err := rt.stashForQuit(); err == nil {
				t.Errorf("%s: stashForQuit() = nil, want an error", name)
			}
		}
	})
	t.Run("nothing unsaved writes nothing", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "big.txt")
		must(t, os.WriteFile(path, []byte(strings.Repeat("some line of text\n", 250)), 0o644))
		ed := editor.New(largeOpts())
		must(t, ed.OpenFile(path))
		sess := filepath.Join(dir, "session")
		rt := &runtime{ed: ed, model: quitModel(ed), sessionDir: sess, stashDir: sess}
		must(t, rt.stashForQuit())
		if _, err := os.Stat(sess); err == nil {
			t.Errorf("a quit with nothing unsaved wrote %s", sess)
		}
	})
}

// indexComplete runs the index jobs of ed until its index is complete.
func indexComplete(t *testing.T, ed *editor.Editor) {
	t.Helper()
	for {
		job, ok := ed.PlanIndex()
		if !ok {
			return
		}
		if !ed.ApplyIndex(job.Run(t.Context())) {
			return
		}
	}
}

func TestCrashRecoveryOverAnAlreadyRestoredLargeDocument(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ed, _ := largeSessionFile(t, dir)
	sess, rec := filepath.Join(dir, "session"), filepath.Join(dir, "recovery")
	must(t, writeSession(ed, sess, ui.ExplorerState{}, nil))
	s, _, err := session.Load(sess)
	must(t, err)

	// The run that restored the session made newer edits, then crashed.
	ed2 := editor.New(largeOpts())
	if failed := restoreSession(ed2, sess, s); len(failed) != 0 {
		t.Fatalf("restoreSession failed: %+v", failed)
	}
	indexComplete(t, ed2)
	must(t, ed2.GotoLine(100, 1))
	ed2.Do("insert_char", map[string]any{"text": "NEWER "})
	want := ed2.Active().Buffer.Text()
	must(t, newRecoveryWriter(ed2, rec).Plan()())

	// The next start restores the session, and recovery must bring the newer edits.
	ed3 := editor.New(largeOpts())
	if failed := restoreSession(ed3, sess, s); len(failed) != 0 {
		t.Fatalf("restoreSession failed: %+v", failed)
	}
	failed, n := recoverCrash(ed3, rec)
	if len(failed) != 0 || n != 1 {
		t.Fatalf("recoverCrash = %+v, %d: want the newer edits restored", failed, n)
	}
	indexComplete(t, ed3)
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
	must(t, os.WriteFile(path, []byte(strings.Repeat("x\n", 2_600_000)), 0o644))
	ed := editor.New(largeOpts())
	must(t, ed.OpenFile(path))
	ed.Do("insert_char", map[string]any{"text": "edit "})
	sess := filepath.Join(dir, "session")
	must(t, writeSession(ed, sess, ui.ExplorerState{}, nil))
	s, _, err := session.Load(sess)
	must(t, err)
	st, err := session.ReadOverlay(sess, s.Files[0].OverlayFile)
	must(t, err)
	// The identity fits the file, but the overlay claims more lines than it has.
	st.Overlay.TailFirst = 50_000_000
	bad, err := session.StashOverlay(sess, st)
	must(t, err)
	s.Files[0].OverlayFile = bad

	ed2 := editor.New(largeOpts())
	if failed := restoreSession(ed2, sess, s); len(failed) != 0 {
		t.Fatalf("restoreSession failed: %+v", failed)
	}
	indexComplete(t, ed2)
	d := ed2.Active()
	if d.Buffer.Modified() || !d.Modified() {
		t.Fatalf("buffer modified %v, document modified %v: want a failed restore that is still unsaved", d.Buffer.Modified(), d.Modified())
	}
	must(t, ed2.GotoLine(1, 1))
	ed2.Do("insert_char", map[string]any{"text": "NEWEDIT "})

	must(t, writeSession(ed2, sess, ui.ExplorerState{}, nil))
	s2, _, err := session.Load(sess)
	must(t, err)
	if len(s2.Files) != 2 || s2.Files[0].Path != path || s2.Files[1].Path != path {
		t.Fatalf("session files = %+v, want the live edits and the failed stash", s2.Files)
	}
	live, err := session.ReadOverlay(sess, s2.Files[0].OverlayFile)
	must(t, err)
	kept, err := session.ReadOverlay(sess, s2.Files[1].OverlayFile)
	must(t, err)
	if live.Overlay.TailFirst == 50_000_000 || kept.Overlay.TailFirst != 50_000_000 {
		t.Errorf("tail of the live edits %d, of the failed stash %d", live.Overlay.TailFirst, kept.Overlay.TailFirst)
	}

	// The next start restores the live edits, and the failed stash is reported and kept.
	ed3 := editor.New(largeOpts())
	failed := restoreSession(ed3, sess, s2)
	if len(failed) != 1 || failed[0].file.OverlayFile != s2.Files[1].OverlayFile {
		t.Fatalf("failed = %+v, want the failed stash kept", failed)
	}
	indexComplete(t, ed3)
	if d := ed3.Active(); len(ed3.Tabs()) != 1 || !strings.HasPrefix(d.Buffer.Line(0), "NEWEDIT ") {
		t.Errorf("tabs %d: the live edits did not come back", len(ed3.Tabs()))
	}
}
