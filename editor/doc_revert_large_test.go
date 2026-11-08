package editor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// replaceFile replaces the file at path with content the way another program does: it
// writes a new file next to it and renames it over, so a reader that has the old file
// open keeps the old bytes.
func replaceFile(t *testing.T, path, content string) {
	t.Helper()
	tmp := path + ".new"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func TestRevertSwapsTheBufferOfAnUnmodifiedLargeDocument(t *testing.T) {
	t.Parallel()
	opts, ledger := ledgerOptions()
	e, d := openIndexed(t, opts, lineText(200))
	d.Cursor, d.Top = buffer.Pos{Line: 7, Col: 2}, 3
	old := d.Buffer
	replaceFile(t, d.Path, "new first line\n"+lineText(250))
	if err := e.Revert(false); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if !d.Large() || d.Buffer == old {
		t.Fatalf("large %v, buffer replaced %v", d.Large(), d.Buffer != old)
	}
	if ledger.live() != 1 {
		t.Errorf("%d sources are open, want only the new file's", ledger.live())
	}
	if d.Buffer.Line(0) != "new first line" || d.Buffer.Line(1) != "line 0000" {
		t.Errorf("lines = %q, %q", d.Buffer.Line(0), d.Buffer.Line(1))
	}
	if d.Status != "Reverted big.txt" || d.Modified() {
		t.Errorf("status %q, modified %v", d.Status, d.Modified())
	}
	if !d.Disk.SameAs(diskStateOf(d.Path)) {
		t.Error("the disk state is not the new file's")
	}
	indexAll(t, e)
	if got := d.Buffer.Lines(); got != 252 {
		t.Errorf("lines = %d, want 252", got)
	}
	if d.Cursor != (buffer.Pos{Line: 7, Col: 2}) {
		t.Errorf("cursor %v", d.Cursor)
	}
	if st := d.Buffer.Stats(); st.OverCap != 0 {
		t.Errorf("OverCap = %d", st.OverCap)
	}
	// The old buffer reads as closed.
	if old.Line(1) != "" {
		t.Error("the old buffer still reads")
	}
}

func TestRevertKeepsACaretBeyondTheIndexedPart(t *testing.T) {
	t.Parallel()
	e, d := openIndexed(t, largeOptions(), lineText(200))
	d.Cursor, d.Top = buffer.Pos{Line: 150, Col: 3}, 148
	d.Sel = &buffer.Selection{Anchor: buffer.Pos{Line: 150}, Head: d.Cursor}
	replaceFile(t, d.Path, lineText(250))
	if err := e.Revert(false); err != nil {
		t.Fatal(err)
	}
	if d.Index().Complete || d.Cursor.Line >= 150 || d.Top >= d.Buffer.Lines() {
		t.Fatalf("cursor %v top %d lines %d complete %v", d.Cursor, d.Top, d.Buffer.Lines(), d.Index().Complete)
	}
	if d.Sel != nil || d.LargeState().Pending == nil {
		t.Fatalf("selection %v, pending %v", d.Sel, d.LargeState().Pending)
	}
	indexAll(t, e)
	if d.Cursor != (buffer.Pos{Line: 150, Col: 3}) {
		t.Errorf("cursor = %v, want the place it was", d.Cursor)
	}
}

func TestRevertRefusesAModifiedLargeDocument(t *testing.T) {
	t.Parallel()
	opts, ledger := ledgerOptions()
	e, d := openIndexed(t, opts, lineText(200))
	d.Buffer.Insert(buffer.Pos{}, "mine ")
	replaceFile(t, d.Path, lineText(250))
	for _, force := range []bool{false, true} {
		_, err := e.PlanRevert(force)
		if !errors.Is(err, ErrLargeFile) {
			t.Fatalf("PlanRevert(%v) = %v, want ErrLargeFile", force, err)
		}
	}
	if !d.Modified() || d.Buffer.Line(0) != "mine line 0000" || ledger.live() != 1 {
		t.Fatal("the refused revert changed the document")
	}
}

func TestRevertOfAFrozenDocumentReloadsIt(t *testing.T) {
	t.Parallel()
	opts, ledger := ledgerOptions()
	e, d := openIndexed(t, opts, lineText(200))
	if err := os.Truncate(d.Path, 1200); err != nil {
		t.Fatal(err)
	}
	if err := d.CheckSource(); !errors.Is(err, buffer.ErrSourceChanged) {
		t.Fatalf("CheckSource = %v", err)
	}
	if !d.ReadOnly() {
		t.Fatal("the frozen document is not read-only")
	}
	if err := e.Revert(false); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if d.ReadOnly() || !d.Large() || ledger.live() != 1 {
		t.Fatalf("read-only %v, large %v, %d sources", d.ReadOnly(), d.Large(), ledger.live())
	}
	indexAll(t, e)
	if err := d.Buffer.SourceErr(); err != nil {
		t.Fatalf("SourceErr = %v", err)
	}
	// A document that the user made read-only is still refused by the revert action.
	d.SetReadOnly(true)
	if r := e.Do("revert", nil); !errors.Is(r.Err, ErrReadOnly) {
		t.Fatalf("revert = %v, want ErrReadOnly", r.Err)
	}
}

func TestRevertOfALargeDocumentThatShrankIsASmallOne(t *testing.T) {
	t.Parallel()
	opts, ledger := ledgerOptions()
	e, d := openIndexed(t, opts, lineText(200))
	replaceFile(t, d.Path, "small now\n")
	if err := e.Revert(false); err != nil {
		t.Fatal(err)
	}
	if d.Large() || d.LargeState() != nil || d.Buffer.Large() {
		t.Fatal("the document is still large")
	}
	if d.Buffer.Text() != "small now\n" || d.Saved != "small now\n" || d.Modified() {
		t.Fatalf("text %q saved %q", d.Buffer.Text(), d.Saved)
	}
	if ledger.live() != 0 {
		t.Errorf("%d sources are open", ledger.live())
	}
	typeText(t, e, "x")
	if err := e.Save(); err != nil || readFile(t, d.Path) != "xsmall now\n" {
		t.Fatalf("Save = %v, file %q", err, readFile(t, d.Path))
	}
	// And the other way: a document that is small is not swapped for a large one.
	replaceFile(t, d.Path, lineText(200))
	if _, err := e.PlanRevert(true); !errors.Is(err, ErrLargeFile) {
		t.Fatalf("PlanRevert = %v, want ErrLargeFile", err)
	}
}

func TestApplyRevertOfAStalePlanReleasesTheFreshFile(t *testing.T) {
	t.Parallel()
	opts, ledger := ledgerOptions()
	e, d := openIndexed(t, opts, lineText(200))
	replaceFile(t, d.Path, lineText(250))
	plan, err := e.PlanRevert(false)
	if err != nil {
		t.Fatal(err)
	}
	res := RunRevert(plan)
	if res.Err != nil || ledger.live() != 2 {
		t.Fatalf("err %v, %d sources", res.Err, ledger.live())
	}
	d.Buffer.Insert(buffer.Pos{}, "x")
	if err := e.ApplyRevert(plan, res); !errors.Is(err, ErrStaleRevert) {
		t.Fatalf("ApplyRevert = %v, want ErrStaleRevert", err)
	}
	if ledger.live() != 1 || !d.Modified() {
		t.Fatalf("%d sources, modified %v", ledger.live(), d.Modified())
	}
	// A plan for a document that is gone releases its file too.
	plan2 := e.PlanRevertPath(d.Path)
	res2 := RunRevert(plan2)
	if err := e.ApplyRevert(plan2, res2); !errors.Is(err, ErrStaleRevert) {
		t.Fatalf("ApplyRevert of a path plan on a modified tab = %v", err)
	}
	if ledger.live() != 1 {
		t.Fatalf("%d sources are open", ledger.live())
	}
}

func TestRevertOfABackgroundLargeTab(t *testing.T) {
	t.Parallel()
	opts, ledger := ledgerOptions()
	e := New(opts)
	big := openLargeNamed(t, e, "big.txt", lineText(200))
	indexAll(t, e)
	if err := e.OpenFile(writeLargeFile(t, "small.txt", "tiny\n")); err != nil {
		t.Fatal(err)
	}
	if e.Active() == big {
		t.Fatal("the small file is not active")
	}
	replaceFile(t, big.Path, "changed\n"+lineText(200))
	plan := e.PlanRevertPath(big.Path)
	res := RunRevert(plan)
	if err := e.ApplyRevert(plan, res); err != nil {
		t.Fatalf("ApplyRevert: %v", err)
	}
	if e.Active() == big || big.Buffer.Line(0) != "changed" || !big.Large() {
		t.Fatal("the background tab was not reloaded")
	}
	if ledger.live() != 1 {
		t.Errorf("%d sources are open", ledger.live())
	}
	// A plan made from the path alone cannot reload a large document.
	if err := e.ApplyRevert(RevertPlan{Path: big.Path}, RunRevert(RevertPlan{Path: big.Path})); !errors.Is(err, ErrLargeFile) {
		t.Fatalf("ApplyRevert = %v, want ErrLargeFile", err)
	}
}

func TestRunRevertDoesNotReadAHugeFileWhole(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "huge.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxFullLoad + 1); err != nil {
		_ = f.Close()
		t.Skipf("no sparse files: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	res := RunRevert(RevertPlan{Path: path})
	if !errors.Is(res.Err, ErrLargeFile) {
		t.Fatalf("err = %v, want ErrLargeFile", res.Err)
	}
}

func TestRevertToAFileThatIsNotUTF8AsksForAFullLoad(t *testing.T) {
	t.Parallel()
	e, d := openIndexed(t, largeOptions(), lineText(200))
	latin := strings.Repeat("caf\xe9 au lait\n", 200)
	replaceFile(t, d.Path, latin)
	err := e.Revert(false)
	var full ErrLargeNeedsFullLoad
	if !errors.As(err, &full) {
		t.Fatalf("Revert = %v, want ErrLargeNeedsFullLoad", err)
	}
	if d.Buffer.Line(0) != "line 0000" || d.Modified() {
		t.Fatal("the failed revert changed the document")
	}
}

func TestRevertActionReloadsAFrozenDocument(t *testing.T) {
	t.Parallel()
	opts, _ := ledgerOptions()
	e, d := openIndexed(t, opts, lineText(200))
	if err := os.Truncate(d.Path, 1200); err != nil {
		t.Fatal(err)
	}
	if err := d.CheckSource(); !errors.Is(err, buffer.ErrSourceChanged) {
		t.Fatalf("CheckSource = %v", err)
	}
	if r := e.Do("revert", nil); r.Err != nil {
		t.Fatalf("revert: %v", r.Err)
	}
	if d.ReadOnly() || !d.Large() {
		t.Fatalf("read-only %v, large %v", d.ReadOnly(), d.Large())
	}
}

func TestRevertOfTheEditorsOwnSaveKeepsTheLargeDocument(t *testing.T) {
	t.Parallel()
	e, d := openLarge(t, largeOptions(), lineText(400))
	indexAll(t, e)
	typeAt(t, e, 10, 0, "A")
	if err := e.GotoLine(20, 1); err != nil {
		t.Fatal(err)
	}
	run(t, e, "select_line", "save")
	if d.Modified() || !d.Index().Complete || !d.HasSelection() {
		t.Fatalf("modified %v, complete %v, selection %v after the save", d.Modified(), d.Index().Complete, d.HasSelection())
	}
	buf, status := d.Buffer, d.Status
	plan := e.PlanRevertPath(d.Path)
	if err := e.ApplyRevert(plan, RunRevert(plan)); err != nil {
		t.Fatalf("ApplyRevert: %v", err)
	}
	if d.Buffer != buf || !d.Index().Complete || !d.HasSelection() {
		t.Errorf("buffer kept %v, index complete %v, selection %v: want the document as it was",
			d.Buffer == buf, d.Index().Complete, d.HasSelection())
	}
	if d.Status != status {
		t.Errorf("status = %q, want %q", d.Status, status)
	}
}
