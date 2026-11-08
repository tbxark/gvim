package uiadapter

import (
	"errors"
	"github.com/tbxark/gvim/internal/apptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/theme"
)

// TestRevertThroughTheAdapterPlansReadsAndApplies checks the revert split on the real
// adapter. The plan reads nothing and changes no text, the run reads the file, and the apply
// replaces the text of the planned document.
func TestRevertThroughTheAdapterPlansReadsAndApplies(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a.txt")
	apptest.Must(t, os.WriteFile(path, []byte("abc\n"), 0o644))
	ed := editor.New(editor.Options{})
	apptest.Must(t, ed.OpenFile(path))
	a := &Editor{ed: ed}
	apptest.Must(t, os.WriteFile(path, []byte("fresh\n"), 0o644))

	plan, err := a.PlanRevert(false)
	if err != nil {
		t.Fatalf("PlanRevert: %v", err)
	}
	if got := ed.Active().Buffer.Text(); got != "abc\n" {
		t.Fatalf("text after the plan = %q, want it unchanged", got)
	}
	res := a.RunRevert(plan)
	if res.Err != nil {
		t.Fatalf("RunRevert: %v", res.Err)
	}
	if err := a.ApplyRevert(plan, res); err != nil {
		t.Fatalf("ApplyRevert: %v", err)
	}
	if got := ed.Active().Buffer.Text(); got != "fresh\n" {
		t.Fatalf("text after the apply = %q, want the file's text", got)
	}
}

// TestRevertPlanIsRefusedWhileASaveWrites checks the gate of the adapter: a revert is not
// planned while a planned write is pending, as a save is not, because the file may be half
// written when it is read.
func TestRevertPlanIsRefusedWhileASaveWrites(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a.txt")
	apptest.Must(t, os.WriteFile(path, []byte("abc\n"), 0o644))
	ed := editor.New(editor.Options{})
	apptest.Must(t, ed.OpenFile(path))
	a := &Editor{ed: ed, saves: &autoSaveGate{writing: true}}
	if _, err := a.PlanRevert(false); !errors.Is(err, errAutoSaveWriting) {
		t.Fatalf("PlanRevert err = %v, want errAutoSaveWriting", err)
	}
}

// TestFormatBufferRowHoldsOnlyWithAFormatter checks the menu's when-clause through the real
// adapter: the Format Buffer row holds when the language of the active buffer has a formatter.
func TestFormatBufferRowHoldsOnlyWithAFormatter(t *testing.T) {
	apptest.IsolateConfig(t)
	path := filepath.Join(t.TempDir(), "a.txt")
	apptest.Must(t, os.WriteFile(path, []byte("abc\n"), 0o644))
	ed := editor.New(editor.Options{})
	ed.SetFormatters(map[string]string{"go": "gofmt"})
	apptest.Must(t, ed.OpenFile(path))
	m := tui.New(NewDeps(ed, keymap.Default(), theme.Theme{}, nil, nil))

	if m.Holds("formatter_available") {
		t.Fatal("a buffer with no formatter holds formatter_available")
	}
	ed.SetLanguage("go")
	if !m.Holds("formatter_available") {
		t.Fatal("a buffer with a formatter does not hold formatter_available")
	}
}
