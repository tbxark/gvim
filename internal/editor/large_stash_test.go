package editor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/session"
)

// typeAt types text at the 0-based line and column of the active document of e.
func typeAt(t *testing.T, e *Editor, line, col int, text string) {
	t.Helper()
	if err := e.GotoLine(line+1, col+1); err != nil {
		t.Fatalf("GotoLine: %v", err)
	}
	if res := e.Do("insert_char", map[string]any{"text": text}); res.Err != nil {
		t.Fatalf("insert_char: %v", res.Err)
	}
}

// stashOf returns the stash of the unsaved edits of d, and fails when there is none.
func stashOf(t *testing.T, d *Document) session.OverlayStash {
	t.Helper()
	st, ok := d.OverlayStash()
	if !ok {
		t.Fatal("OverlayStash() found nothing to stash")
	}
	return st
}

// sameFileOpened opens the file of d again in a new editor made from opts, as a new
// session does.
func sameFileOpened(t *testing.T, opts Options, path string) (*Editor, *Document) {
	t.Helper()
	e := New(opts)
	if err := e.OpenFile(path); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	return e, e.Active()
}

func TestOverlayStashFollowsTheEdits(t *testing.T) {
	t.Parallel()
	e, d := openLarge(t, largeOptions(), lineText(150))
	indexAll(t, e)
	if _, ok := d.OverlayStash(); ok {
		t.Fatal("an unedited document has a stash")
	}
	typeAt(t, e, 100, 0, "edited ")
	st := stashOf(t, d)
	info, err := os.Stat(d.Path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Path != d.Path || st.Size != info.Size() || st.MTimeUnixNano != info.ModTime().UnixNano() || st.Skip != 0 || st.EOL != eolLF {
		t.Errorf("identity = %+v, want path %s size %d mtime %d", st, d.Path, info.Size(), info.ModTime().UnixNano())
	}
	if st.Overlay.Empty() {
		t.Errorf("the stash holds no edits")
	}
	e.Do("undo", nil)
	if _, ok := d.OverlayStash(); ok {
		t.Errorf("a document whose edits were undone has a stash")
	}
	small := New(Options{})
	small.Do("insert_char", map[string]any{"text": "x"})
	if _, ok := small.Active().OverlayStash(); ok {
		t.Errorf("a document that is not large has an overlay stash")
	}
}

func TestOverlayStashKeepsTheByteOrderMark(t *testing.T) {
	t.Parallel()
	e, d := openLarge(t, largeOptions(), "\xef\xbb\xbf"+lineText(150))
	indexAll(t, e)
	typeAt(t, e, 3, 0, "x")
	if st := stashOf(t, d); st.Skip != 3 {
		t.Errorf("Skip = %d, want 3", st.Skip)
	}
}

func TestRestoreOverlayWaitsForTheIndexAndRefusesEditsMeanwhile(t *testing.T) {
	t.Parallel()
	opts := largeOptions()
	e, d := openLarge(t, opts, lineText(150))
	indexAll(t, e)
	typeAt(t, e, 100, 0, "edited ")
	e.Do("move_document_start", nil)
	typeAt(t, e, 2, 4, "!")
	e.Do("insert_newline", nil)
	st := stashOf(t, d)
	want := d.Buffer.Text()

	fresh, nd := sameFileOpened(t, opts, d.Path)
	if nd.Index().Complete {
		t.Fatal("the index of the new document is complete: the test needs a pending restore")
	}
	if err := fresh.RestoreOverlay(nd, st); err != nil {
		t.Fatalf("RestoreOverlay: %v", err)
	}
	if !nd.restoring() || nd.Buffer.Modified() {
		t.Fatalf("restoring %v, modified %v: want a restore that waits", nd.restoring(), nd.Buffer.Modified())
	}
	if !strings.Contains(nd.status, "Restoring unsaved changes") {
		t.Errorf("status = %q", nd.status)
	}
	// The unsaved edits are still stashed if the session is saved now.
	if again, ok := nd.OverlayStash(); !ok || again.Overlay.TailFirst != st.Overlay.TailFirst {
		t.Errorf("a document waiting for its stash does not keep it: %+v, %v", again, ok)
	}
	if got := fresh.UnsavedDocs(); len(got) != 1 || got[0] != nd {
		t.Errorf("UnsavedDocs() = %v, want the document that waits", got)
	}
	for _, action := range []Action{"insert_char", "delete_backward", "undo", "paste"} {
		res := fresh.Do(action, map[string]any{"text": "z"})
		if !errors.Is(res.Err, ErrRestoring) {
			t.Errorf("%s while restoring: %v, want ErrRestoring", action, res.Err)
		}
	}
	if nd.Buffer.Modified() {
		t.Fatal("a refused action changed the text")
	}
	// Moving around is still allowed.
	if res := fresh.Do("move_down", nil); res.Err != nil {
		t.Errorf("move_down while restoring: %v", res.Err)
	}

	steps := 0
	for nd.restoring() {
		ran, _ := indexStep(fresh)
		if !ran || steps > 1000 {
			t.Fatalf("the restore did not finish after %d index steps", steps)
		}
		steps++
	}
	if steps < 2 {
		t.Errorf("the restore finished after %d index steps: it did not wait", steps)
	}
	if !nd.Buffer.Modified() {
		t.Fatalf("the document is not modified after the restore")
	}
	if !strings.Contains(nd.status, "Restored unsaved changes") {
		t.Errorf("status = %q", nd.status)
	}
	indexAll(t, fresh)
	if got := nd.Buffer.Text(); got != want {
		t.Errorf("the restored text differs:\n got %q\nwant %q", got, want)
	}
	typeAt(t, fresh, 0, 0, "again ")
	if !strings.HasPrefix(nd.Buffer.Line(0), "again ") {
		t.Errorf("a restored document refuses edits: line 0 = %q", nd.Buffer.Line(0))
	}
}

func TestSaveAndRevertWaitForTheRestore(t *testing.T) {
	t.Parallel()
	opts := largeOptions()
	e, d := openLarge(t, opts, lineText(150))
	indexAll(t, e)
	typeAt(t, e, 100, 0, "edited ")
	st := stashOf(t, d)

	fresh, nd := sameFileOpened(t, opts, d.Path)
	if err := fresh.RestoreOverlay(nd, st); err != nil || !nd.restoring() {
		t.Fatalf("RestoreOverlay() = %v, restoring %v: want a restore that waits", err, nd.restoring())
	}
	plan, _ := fresh.PlanSave()
	for _, r := range plan.Run() {
		if !errors.Is(r.Err(), ErrRestoring) {
			t.Errorf("save while restoring: %v, want ErrRestoring", r.Err())
		}
	}
	if _, err := fresh.PlanRevert(true); !errors.Is(err, ErrRestoring) {
		t.Errorf("PlanRevert while restoring: %v, want ErrRestoring", err)
	}
	if again, ok := nd.OverlayStash(); !ok || again.Overlay.TailFirst != st.Overlay.TailFirst {
		t.Errorf("the stash was lost: %+v, %v", again, ok)
	}
}

func TestAJumpAskedWhileRestoringStillLands(t *testing.T) {
	t.Parallel()
	opts := largeOptions()
	e, d := openLarge(t, opts, lineText(150))
	indexAll(t, e)
	typeAt(t, e, 100, 0, "edited ")
	st := stashOf(t, d)

	fresh, nd := sameFileOpened(t, opts, d.Path)
	if err := fresh.RestoreOverlay(nd, st); err != nil || !nd.restoring() {
		t.Fatalf("RestoreOverlay() = %v, restoring %v: want a restore that waits", err, nd.restoring())
	}
	// The session puts the caret where it was, far past the lines the index knows.
	if err := fresh.GotoLine(140, 3); err != nil {
		t.Fatal(err)
	}
	indexAll(t, fresh)
	if nd.restoring() || !nd.Buffer.Modified() {
		t.Fatalf("restoring %v, modified %v: want the edits back", nd.restoring(), nd.Buffer.Modified())
	}
	if nd.Cursor.Line != 139 || nd.Cursor.Col != 2 {
		t.Errorf("cursor = %+v, want line 139, col 2", nd.Cursor)
	}
}

func TestRestoreOverlayDoneAtOnceWhenTheIndexIsFarEnough(t *testing.T) {
	t.Parallel()
	opts := largeOptions()
	e, d := openLarge(t, opts, lineText(150))
	indexAll(t, e)
	typeAt(t, e, 1, 0, "e")
	st := stashOf(t, d)
	fresh, nd := sameFileOpened(t, opts, d.Path)
	if err := fresh.RestoreOverlay(nd, st); err != nil {
		t.Fatalf("RestoreOverlay: %v", err)
	}
	if nd.restoring() || !nd.Buffer.Modified() || nd.Buffer.Line(1) != "eline 0001" {
		t.Errorf("restoring %v, modified %v, line 1 = %q: want the edit back at once",
			nd.restoring(), nd.Buffer.Modified(), nd.Buffer.Line(1))
	}
	if res := fresh.Do("undo", nil); res.Err != nil || nd.Buffer.Modified() {
		t.Errorf("undo of the restore: %v, modified %v", res.Err, nd.Buffer.Modified())
	}
}

func TestRestoreOverlayRefusesAFileThatChanged(t *testing.T) {
	t.Parallel()
	opts := largeOptions()
	e, d := openLarge(t, opts, lineText(150))
	indexAll(t, e)
	typeAt(t, e, 5, 0, "x")
	st := stashOf(t, d)

	tests := []struct {
		name   string
		change func(t *testing.T, path string)
		stash  func(st *session.OverlayStash)
	}{
		{"other size", func(t *testing.T, path string) {
			if err := os.WriteFile(path, []byte(lineText(151)), 0o600); err != nil {
				t.Fatal(err)
			}
		}, nil},
		{"other time", func(t *testing.T, path string) {
			if err := os.Chtimes(path, time.Now(), time.Now().Add(-time.Hour)); err != nil {
				t.Fatal(err)
			}
		}, nil},
		{"other byte-order mark", func(*testing.T, string) {}, func(st *session.OverlayStash) { st.Skip = 3 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "big.txt")
			if err := os.WriteFile(path, []byte(lineText(150)), 0o600); err != nil {
				t.Fatal(err)
			}
			// The stash was taken from a file with the time of the document above, so
			// this copy of the file fits only when the time is the same.
			info, err := os.Stat(d.Path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
			tc.change(t, path)
			fresh, nd := sameFileOpened(t, opts, path)
			in := st
			if tc.stash != nil {
				tc.stash(&in)
			}
			if err := fresh.RestoreOverlay(nd, in); !errors.Is(err, ErrStashStale) {
				t.Fatalf("RestoreOverlay() = %v, want ErrStashStale", err)
			}
			if nd.Buffer.Modified() || nd.restoring() {
				t.Errorf("a refused stash changed the document")
			}
		})
	}

	t.Run("same file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "big.txt")
		if err := os.WriteFile(path, []byte(lineText(150)), 0o600); err != nil {
			t.Fatal(err)
		}
		info, _ := os.Stat(d.Path)
		if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
			t.Fatal(err)
		}
		fresh, nd := sameFileOpened(t, opts, path)
		if err := fresh.RestoreOverlay(nd, st); err != nil {
			t.Fatalf("RestoreOverlay() = %v for a copy of the same file", err)
		}
	})
}

func TestRestoreOverlayRefusals(t *testing.T) {
	t.Parallel()
	opts := largeOptions()
	e, d := openLarge(t, opts, lineText(150))
	indexAll(t, e)
	typeAt(t, e, 5, 0, "x")
	st := stashOf(t, d)

	t.Run("a document that is not large", func(t *testing.T) {
		small := New(Options{})
		small.Do("insert_char", map[string]any{"text": "x"})
		if err := small.RestoreOverlay(small.Active(), st); err == nil {
			t.Errorf("RestoreOverlay() into a small document succeeded")
		}
	})
	t.Run("a document with edits", func(t *testing.T) {
		fresh, nd := sameFileOpened(t, opts, d.Path)
		typeAt(t, fresh, 0, 0, "mine ")
		if err := fresh.RestoreOverlay(nd, st); err == nil {
			t.Errorf("RestoreOverlay() over edits succeeded")
		}
		if !strings.HasPrefix(nd.Buffer.Line(0), "mine ") {
			t.Errorf("the refused restore changed the text: %q", nd.Buffer.Line(0))
		}
	})
}

func TestARestoreThatFailsLateKeepsTheStashAndFreesTheDocument(t *testing.T) {
	t.Parallel()
	opts := largeOptions()
	e, d := openLarge(t, opts, lineText(150))
	indexAll(t, e)
	typeAt(t, e, 5, 0, "x")
	st := stashOf(t, d)
	// The identity fits, but the overlay claims more lines than the file has.
	st.Overlay.TailFirst = 5000

	fresh, nd := sameFileOpened(t, opts, d.Path)
	if err := fresh.RestoreOverlay(nd, st); err != nil {
		t.Fatalf("RestoreOverlay: %v", err)
	}
	indexAll(t, fresh)
	if nd.restoring() {
		t.Fatalf("the document still refuses edits after the index is complete")
	}
	if !strings.Contains(nd.status, "could not be restored") {
		t.Errorf("status = %q, want the failure", nd.status)
	}
	if kept, ok := nd.OverlayStash(); !ok || kept.Overlay.TailFirst != 5000 {
		t.Errorf("the stash that could not be restored is not kept: %+v, %v", kept, ok)
	}
	if nd.Buffer.Modified() {
		t.Errorf("a failed restore left the text modified")
	}
	typeAt(t, fresh, 0, 0, "ok ")
}

func TestSourceChangedWritesStash(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	opts := largeOptions()
	opts.SessionDir = dir
	e, d := openLarge(t, opts, lineText(150))
	indexAll(t, e)
	typeAt(t, e, 5, 0, "x")
	if err := os.Truncate(d.Path, 600); err != nil {
		t.Fatal(err)
	}
	if err := d.CheckSource(); !errors.Is(err, buffer.ErrSourceChanged) {
		t.Fatalf("CheckSource() = %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "overlay-*.json"))
	if len(files) != 1 {
		t.Fatalf("%d overlay stashes were written, want 1", len(files))
	}
	if !strings.Contains(d.status, files[0]) || !strings.Contains(d.status, "File changed on disk") {
		t.Errorf("status = %q, want the change and the stash path", d.status)
	}
	got, err := session.ReadOverlay(dir, filepath.Base(files[0]))
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != d.Path || got.Overlay.Empty() {
		t.Errorf("the stash = %+v, want the edits of %s", got, d.Path)
	}
	// The disk poll calls CheckSource again and again: the stash is written once.
	_ = d.CheckSource()
	_ = d.stashUnsaved()
	if again, _ := filepath.Glob(filepath.Join(dir, "overlay-*.json")); len(again) != 1 {
		t.Errorf("%d overlay stashes after more checks, want 1", len(again))
	}
}

func TestAGrownSourceWritesTheStashToo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	opts := largeOptions()
	opts.SessionDir = dir
	e, d, src := openMem(t, opts, lineText(150))
	indexAll(t, e)
	typeAt(t, e, 5, 0, "x")
	src.Grow([]byte("appended\n"))
	if err := d.CheckSource(); !errors.Is(err, buffer.ErrSourceGrew) {
		t.Fatalf("CheckSource() = %v", err)
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "overlay-*.json")); len(files) != 1 {
		t.Fatalf("%d overlay stashes were written, want 1", len(files))
	}
	if !strings.Contains(d.status, "Unsaved changes stashed in") {
		t.Errorf("status = %q", d.status)
	}
}

func TestAnUneditedDocumentWritesNoStashWhenTheSourceChanges(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	opts := largeOptions()
	opts.SessionDir = dir
	_, d, src := openMem(t, opts, lineText(150))
	src.Truncate(100)
	_ = d.CheckSource()
	if files, _ := filepath.Glob(filepath.Join(dir, "*")); len(files) != 0 {
		t.Errorf("stash files %v were written for a document with no edits", files)
	}
}

func TestAFailedIndexWritesTheStashOnce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	opts := largeOptions()
	opts.SessionDir = dir
	e, d, src := openMem(t, opts, lineText(150))
	// Edit the lines the first batch has, then let the next read fail.
	typeAt(t, e, 1, 0, "x")
	src.FailNext(1, errors.New("disk gone"))
	for range 20 {
		if ran, more := indexStep(e); !ran || !more {
			break
		}
	}
	if !strings.HasPrefix(d.status, "Index failed:") {
		t.Fatalf("status = %q, want the index failure", d.status)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "overlay-*.json"))
	if len(files) != 1 || !strings.Contains(d.status, files[0]) {
		t.Errorf("stash files %v, status %q: want one stash named in the status", files, d.status)
	}
}

func TestARebaseMovesTheIdentityOfTheStashToTheSavedFile(t *testing.T) {
	t.Parallel()
	opts := largeOptions()
	e, d := openLarge(t, opts, lineText(150))
	indexAll(t, e)
	typeAt(t, e, 5, 0, "x")
	if err := e.Save(); err != nil {
		t.Fatal(err)
	}
	if d.large.rebaseFailed {
		t.Fatal("the save did not rebase")
	}
	typeAt(t, e, 7, 0, "y")
	st := stashOf(t, d)
	info, err := os.Stat(d.Path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size != info.Size() || st.MTimeUnixNano != info.ModTime().UnixNano() {
		t.Errorf("identity = size %d mtime %d, want the saved file's %d, %d",
			st.Size, st.MTimeUnixNano, info.Size(), info.ModTime().UnixNano())
	}
	fresh, nd := sameFileOpened(t, opts, d.Path)
	if err := fresh.RestoreOverlay(nd, st); err != nil {
		t.Fatalf("RestoreOverlay() onto the saved file: %v", err)
	}
	indexAll(t, fresh)
	if nd.Buffer.Text() != d.Buffer.Text() {
		t.Errorf("the restored text differs from the edited one")
	}
}
