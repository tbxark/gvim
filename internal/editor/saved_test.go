package editor

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// openTemp writes content to a new file in a temp dir and opens it.
func openTemp(t *testing.T, content string) (*Editor, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "saved.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	e := New(Options{})
	if err := e.OpenFile(path); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	return e, path
}

func TestSavedLinesUnchangedAfterOpen(t *testing.T) {
	t.Parallel()
	e, _ := openTemp(t, "one\ntwo\nthree")
	d := e.Active()
	want := []string{"one", "two", "three"}
	if got := d.SavedLines(); !slices.Equal(got, want) {
		t.Fatalf("SavedLines() = %q, want %q", got, want)
	}
	if got := d.LinesChangedSinceSave(); got != nil {
		t.Fatalf("LinesChangedSinceSave() after open = %v, want nil", got)
	}
}

func TestSavedLinesTrailingNewlineKeepsFinalEmptyLine(t *testing.T) {
	t.Parallel()
	e, _ := openTemp(t, "a\nb\n")
	d := e.Active()
	want := []string{"a", "b", ""}
	if got := d.SavedLines(); !slices.Equal(got, want) {
		t.Fatalf("SavedLines() = %q, want %q", got, want)
	}
	if got := d.LinesChangedSinceSave(); got != nil {
		t.Fatalf("LinesChangedSinceSave() = %v, want nil", got)
	}
}

func TestSavedLinesChangedAfterEdit(t *testing.T) {
	t.Parallel()
	e, _ := openTemp(t, "one\ntwo\nthree")
	d := e.Active()
	run(t, e, "move_document_start", "move_line_end")
	typeText(t, e, "!")
	if got := d.LinesChangedSinceSave(); !slices.Equal(got, []int{0}) {
		t.Fatalf("LinesChangedSinceSave() after edit = %v, want [0]", got)
	}
	// The edit must not move the baseline: it is still the text at open.
	want := []string{"one", "two", "three"}
	if got := d.SavedLines(); !slices.Equal(got, want) {
		t.Fatalf("SavedLines() after edit = %q, want %q", got, want)
	}
}

func TestSavedLinesResetAfterSave(t *testing.T) {
	t.Parallel()
	e, path := openTemp(t, "one\ntwo")
	d := e.Active()
	run(t, e, "move_document_end")
	typeText(t, e, "x\ny")
	run(t, e, "save")
	want := []string{"one", "twox", "y"}
	if got := d.SavedLines(); !slices.Equal(got, want) {
		t.Fatalf("SavedLines() after save = %q, want %q", got, want)
	}
	if got := d.LinesChangedSinceSave(); got != nil {
		t.Fatalf("LinesChangedSinceSave() after save = %v, want nil", got)
	}
	// A later edit is measured against the new baseline, not the open text.
	run(t, e, "move_document_start")
	typeText(t, e, "1")
	if got := d.LinesChangedSinceSave(); !slices.Equal(got, []int{0}) {
		t.Fatalf("LinesChangedSinceSave() after post-save edit = %v, want [0]", got)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != "one\ntwox\ny" {
		t.Fatalf("file = %q, %v; want the saved bytes", b, err)
	}
}

func TestSavedLinesReturnsCopy(t *testing.T) {
	t.Parallel()
	e, _ := openTemp(t, "one\ntwo")
	d := e.Active()
	got := d.SavedLines()
	got[0] = "changed by caller"
	if again := d.SavedLines(); again[0] != "one" {
		t.Fatalf("baseline changed through returned slice: %q", again)
	}
	if changed := d.LinesChangedSinceSave(); changed != nil {
		t.Fatalf("LinesChangedSinceSave() = %v, want nil", changed)
	}
}

func TestSavedLinesUnnamedBuffer(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	d := e.Active()
	if got := d.SavedLines(); !slices.Equal(got, []string{""}) {
		t.Fatalf("SavedLines() of new unnamed buffer = %q, want [\"\"]", got)
	}
	typeText(t, e, "hi")
	if got := d.LinesChangedSinceSave(); !slices.Equal(got, []int{0}) {
		t.Fatalf("LinesChangedSinceSave() after typing = %v, want [0]", got)
	}
}

func TestSavedLinesClosedBufferDiscardsEdits(t *testing.T) {
	t.Parallel()
	e, path := openTemp(t, "one\ntwo")
	run(t, e, "move_document_start")
	typeText(t, e, "x")
	if err := e.CloseTab(0, true); err != nil {
		t.Fatalf("CloseTab force: %v", err)
	}
	// Reopening reads the file from disk, so the baseline is the on-disk text
	// and not the discarded edit.
	if err := e.OpenFile(path); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	d := e.Active()
	if want := []string{"one", "two"}; !slices.Equal(d.SavedLines(), want) {
		t.Fatalf("SavedLines() after reopen = %q, want %q", d.SavedLines(), want)
	}
	if got := d.LinesChangedSinceSave(); got != nil {
		t.Fatalf("LinesChangedSinceSave() after reopen = %v, want nil", got)
	}
}
