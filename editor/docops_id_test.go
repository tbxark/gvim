package editor

import (
	"errors"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

func TestUndoRedoIDWorkOnAnyTab(t *testing.T) {
	t.Parallel()
	e, ids := tabsWith("one", "two")
	// Edit the second tab, which is not active, as one undo step.
	err := e.ApplyEdits(ids[1], []LSPEdit{lspEdit(0, 0, 0, 0, "X"), lspEdit(0, 3, 0, 3, "Y")}, EditOptions{})
	if err != nil {
		t.Fatal(err)
	}
	other, _, _ := e.DocByID(ids[1])
	if got := other.Buffer.Text(); got != "XtwoY" {
		t.Fatalf("text = %q", got)
	}
	if e.ActiveID() != ids[0] {
		t.Fatal("the edit changed the active tab")
	}

	ok, err := e.UndoID(ids[1])
	if err != nil || !ok || other.Buffer.Text() != "two" {
		t.Fatalf("UndoID = %v, %v, text %q, want the text before the edit", ok, err, other.Buffer.Text())
	}
	if ok, err := e.UndoID(ids[1]); err != nil || ok {
		t.Fatalf("UndoID with nothing to undo = %v, %v, want false", ok, err)
	}
	if ok, err := e.RedoID(ids[1]); err != nil || !ok || other.Buffer.Text() != "XtwoY" {
		t.Fatalf("RedoID = %v, %v, text %q", ok, err, other.Buffer.Text())
	}
	if ok, _ := e.RedoID(ids[1]); ok {
		t.Fatal("RedoID with nothing to redo reported a redo")
	}
	if got, _, _ := e.DocByID(ids[0]); got.Buffer.Text() != "one" {
		t.Fatalf("the active tab changed: %q", got.Buffer.Text())
	}
}

func TestUndoIDRefusals(t *testing.T) {
	t.Parallel()
	e, ids := tabsWith("one")
	if _, err := e.UndoID(999); !errors.Is(err, ErrNotOpen) {
		t.Errorf("UndoID(unknown) = %v, want ErrNotOpen", err)
	}
	d, _, _ := e.DocByID(ids[0])
	d.SetReadOnly(true)
	if _, err := e.UndoID(ids[0]); !errors.Is(err, ErrReadOnly) {
		t.Errorf("UndoID on a read-only document = %v, want ErrReadOnly", err)
	}
	if _, err := e.RedoID(ids[0]); !errors.Is(err, ErrReadOnly) {
		t.Errorf("RedoID on a read-only document = %v, want ErrReadOnly", err)
	}
}

func TestRevealID(t *testing.T) {
	t.Parallel()
	e := New(Options{Rows: 10, Cols: 40})
	d := e.Active()
	for range 99 {
		d.Buffer.Insert(d.Buffer.End(), "line\n")
	}
	id := d.ID()
	tests := []struct {
		Name    string
		Line    int
		center  bool
		wantTop func(top int) bool
	}{
		{"already in view: no scroll", 3, false, func(top int) bool { return top == 0 }},
		{"far below: the caret line goes to the middle", 60, false, func(top int) bool { return top == 60-5 }},
		{"center puts the line in the middle even when in view", 3, true, func(top int) bool { return top == 0 }},
		{"center far below", 80, true, func(top int) bool { return top == 80-5 }},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			d.Top = 0
			d.Cursor = buffer.Pos{Line: tt.Line}
			d.Seen = d.Cursor
			if tt.Line == 60 {
				d.Seen = buffer.Pos{} // the caret moved: the margin applies
			}
			if err := e.RevealID(id, tt.center); err != nil {
				t.Fatal(err)
			}
			if !tt.wantTop(d.Top) {
				t.Fatalf("top = %d", d.Top)
			}
		})
	}
	if err := e.RevealID(12345, false); !errors.Is(err, ErrNotOpen) {
		t.Errorf("RevealID(unknown) = %v", err)
	}
}

func TestDocumentPreview(t *testing.T) {
	t.Parallel()
	e, _ := tabsWith("a")
	if e.Active().Preview() {
		t.Fatal("a new tab is a preview")
	}
	e.Active().SetPreview(true)
	if !e.Active().Preview() {
		t.Fatal("Preview does not follow the flag")
	}
}
