package editor

import (
	"errors"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

// An editor started empty has no tab, and every method is safe without one.
func TestEmptyEditorHasNoDocumentAndNoPanics(t *testing.T) {
	t.Parallel()
	e := New(Options{Empty: true})
	if !e.Empty() || e.Active() != nil || len(e.Tabs()) != 0 {
		t.Fatalf("empty = %v, active = %v, tabs = %v", e.Empty(), e.Active(), e.Tabs())
	}
	e.SetViewport(10, 40)
	e.SetCursor(buffer.Pos{}, false)
	e.SelectRange(buffer.Pos{}, buffer.Pos{Col: 1})
	e.ExtendSelection(buffer.Pos{}, UnitWord)
	e.ScrollBy(3)
	e.ScrollTo(2)
	e.SetLanguage("go")
	e.ActivateTab(0)
	if err := e.SaveAs("/tmp/never-written.txt"); !errors.Is(err, ErrNoDocument) {
		t.Errorf("SaveAs = %v, want ErrNoDocument", err)
	}
	if err := e.Save(); !errors.Is(err, ErrNoDocument) {
		t.Errorf("Save = %v, want ErrNoDocument", err)
	}
	if err := e.GotoLine(3, 1); !errors.Is(err, ErrNoDocument) {
		t.Errorf("GotoLine = %v, want ErrNoDocument", err)
	}
	if !e.Empty() {
		t.Fatal("no-op methods opened a document")
	}
}

func TestEmptyEditorDoRefusesDocumentActionsWithStatus(t *testing.T) {
	t.Parallel()
	for _, a := range []Action{"insert_char", "undo", "redo", "copy", "paste", "move_left", "select_all"} {
		e := New(Options{Empty: true})
		res := e.Do(a, map[string]any{"text": "x"})
		if !errors.Is(res.Err, ErrNoDocument) {
			t.Errorf("Do(%s) = %v, want ErrNoDocument", a, res.Err)
		}
		if e.Status() != ErrNoDocument.Error() {
			t.Errorf("Do(%s) status = %q", a, e.Status())
		}
	}
}

func TestEmptyEditorTabActionsDoNotPanic(t *testing.T) {
	t.Parallel()
	e := New(Options{Empty: true})
	for _, a := range []Action{"next_buffer", "prev_buffer", "close_tab", "save_as", "remove_secondary_cursors", "goto_line"} {
		res := e.Do(a, nil)
		if a == "close_tab" && !errors.Is(res.Err, ErrNoDocument) {
			t.Errorf("Do(close_tab) on an empty editor = %v, want ErrNoDocument", res.Err)
		}
		if a == "save_as" && res.Err == nil {
			t.Errorf("Do(save_as) on an empty editor returned no error")
		}
	}
	if err := e.CloseTab(0, false); !errors.Is(err, ErrNoTab) {
		t.Errorf("CloseTab(0) = %v, want ErrNoTab", err)
	}
	if !e.Empty() {
		t.Fatal("tab actions opened a document")
	}
}

// New is the first document of an empty editor, and the last close returns the
// editor to the empty state.
func TestNewOpensUntitledInEmptyEditor(t *testing.T) {
	t.Parallel()
	e := New(Options{Empty: true})
	if res := e.Do("new", nil); res.Err != nil {
		t.Fatalf("new: %v", res.Err)
	}
	if e.Empty() || e.Active() == nil || e.Active().Name() != "Untitled-1" {
		t.Fatalf("after new: tabs = %+v", e.Tabs())
	}
	if e.Status() != "New buffer" {
		t.Errorf("status = %q, want New buffer", e.Status())
	}
	if res := e.Do("insert_char", map[string]any{"text": "hi"}); res.Err != nil {
		t.Fatalf("typing: %v", res.Err)
	}
	if err := e.CloseTab(0, false); !errors.Is(err, ErrUnsaved) {
		t.Fatalf("closing a dirty untitled tab = %v, want ErrUnsaved", err)
	}
	if err := e.CloseTab(0, true); err != nil {
		t.Fatalf("forced close: %v", err)
	}
	if !e.Empty() || e.Status() != "" {
		t.Fatalf("after closing the last tab: empty = %v, status = %q", e.Empty(), e.Status())
	}
}

// A file opened into an empty editor is the only tab, and it is not replaced.
func TestOpenFileIntoEmptyEditor(t *testing.T) {
	t.Parallel()
	path := writeFile(t, t.TempDir(), "a.txt")
	e := New(Options{Empty: true})
	if err := e.OpenFile(path); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if e.Empty() || len(e.Tabs()) != 1 || e.Active().Name() != "a.txt" {
		t.Fatalf("tabs = %+v", e.Tabs())
	}
}
