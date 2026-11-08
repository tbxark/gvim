package editor

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStatusIsErrorTellsFailuresFromNotes checks that a failed save marks the status as
// an error and that the notes that follow it, and the one before, are not errors.
func TestStatusIsErrorTellsFailuresFromNotes(t *testing.T) {
	t.Parallel()
	e, path := openTemp(t, "one\n")
	if e.StatusIsError() {
		t.Fatalf("status %q of a fresh open is an error", e.Status())
	}
	if err := e.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if e.Status() == "" || e.StatusIsError() {
		t.Errorf("after a save the status is %q, error %v, want a note", e.Status(), e.StatusIsError())
	}
	if err := os.RemoveAll(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	e.Active().Buffer.Insert(e.Active().Cursor, "x")
	if err := e.Save(); err == nil {
		t.Fatal("saving into a removed directory did not fail")
	}
	if !e.StatusIsError() {
		t.Errorf("status %q after the failed save is not marked as an error", e.Status())
	}
	e.ClearStatus()
	e.setStatus("Copied")
	if e.StatusIsError() {
		t.Errorf("status %q after a note is marked as an error", e.Status())
	}
}
