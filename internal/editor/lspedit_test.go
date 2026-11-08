package editor

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

func TestApplyLSPEdits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		text  string
		edits []LSPEdit
		want  string
	}{
		{
			name:  "replace one word",
			text:  "hello world\n",
			edits: []LSPEdit{{From: buffer.Pos{Line: 0, Col: 6}, To: buffer.Pos{Line: 0, Col: 11}, Text: "gvim"}},
			want:  "hello gvim\n",
		},
		{
			name: "several edits apply in any order",
			text: "a b c\n",
			edits: []LSPEdit{
				{From: buffer.Pos{Line: 0, Col: 0}, To: buffer.Pos{Line: 0, Col: 1}, Text: "x"},
				{From: buffer.Pos{Line: 0, Col: 4}, To: buffer.Pos{Line: 0, Col: 5}, Text: "z"},
			},
			want: "x b z\n",
		},
		{
			name:  "insertion at a point",
			text:  "ab\n",
			edits: []LSPEdit{{From: buffer.Pos{Line: 0, Col: 1}, To: buffer.Pos{Line: 0, Col: 1}, Text: "-"}},
			want:  "a-b\n",
		},
		{
			name:  "range across lines",
			text:  "one\ntwo\nthree\n",
			edits: []LSPEdit{{From: buffer.Pos{Line: 0, Col: 0}, To: buffer.Pos{Line: 2, Col: 5}, Text: "X"}},
			want:  "X\n",
		},
		{
			name:  "position past the end is clamped",
			text:  "abc\n",
			edits: []LSPEdit{{From: buffer.Pos{Line: 0, Col: 1}, To: buffer.Pos{Line: 9, Col: 0}, Text: "!"}},
			want:  "a!",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e, path := openFixture(t, Options{}, tt.text)
			if err := e.ApplyLSPEdits(path, tt.edits); err != nil {
				t.Fatalf("ApplyLSPEdits: %v", err)
			}
			if got := e.Active().Buffer.Text(); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyLSPEditsIsOneUndoStep(t *testing.T) {
	t.Parallel()
	e, path := openFixture(t, Options{}, "a b c\n")
	edits := []LSPEdit{
		{From: buffer.Pos{Line: 0, Col: 0}, To: buffer.Pos{Line: 0, Col: 1}, Text: "x"},
		{From: buffer.Pos{Line: 0, Col: 4}, To: buffer.Pos{Line: 0, Col: 5}, Text: "z"},
	}
	if err := e.ApplyLSPEdits(path, edits); err != nil {
		t.Fatal(err)
	}
	d := e.Active()
	if _, ok := d.Buffer.Undo(); !ok {
		t.Fatal("Undo reported nothing to undo")
	}
	if got := d.Buffer.Text(); got != "a b c\n" {
		t.Fatalf("after one undo text = %q, want the original text", got)
	}
}

func TestApplyLSPEditsRefusesReadOnlyAndUnknownPath(t *testing.T) {
	t.Parallel()
	e, path := openFixture(t, Options{}, "abc\n")
	edit := []LSPEdit{{From: buffer.Pos{}, To: buffer.Pos{}, Text: "x"}}

	if err := e.ApplyLSPEdits(filepath.Join(t.TempDir(), "other.txt"), edit); !errors.Is(err, ErrNotOpen) {
		t.Fatalf("unknown path: err = %v, want ErrNotOpen", err)
	}

	run(t, e, "toggle_read_only")
	if err := e.ApplyLSPEdits(path, edit); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("read-only: err = %v, want ErrReadOnly", err)
	}
	if got := e.Active().Buffer.Text(); got != "abc\n" {
		t.Fatalf("read-only text changed to %q", got)
	}
}

func TestApplyLSPEditsClampsTheCaret(t *testing.T) {
	t.Parallel()
	e, path := openFixture(t, Options{}, "one\ntwo\nthree\n")
	d := e.Active()
	d.Cursor = buffer.Pos{Line: 2, Col: 4}
	edit := []LSPEdit{{From: buffer.Pos{Line: 0, Col: 0}, To: buffer.Pos{Line: 2, Col: 5}, Text: "x"}}
	if err := e.ApplyLSPEdits(path, edit); err != nil {
		t.Fatal(err)
	}
	if d.Cursor.Line > d.Buffer.Lines()-1 || d.Cursor.Col > d.Buffer.LineLen(d.Cursor.Line) {
		t.Fatalf("caret %+v is outside the text %q", d.Cursor, d.Buffer.Text())
	}
	if d.Sel != nil {
		t.Fatalf("selection survived the edit: %+v", d.Sel)
	}
}

// TestApplyLSPEditsKeepsTheCaretOnItsText checks that a format that adds or removes lines above
// the caret moves the caret with its text, instead of leaving it at the same line and column.
func TestApplyLSPEditsKeepsTheCaretOnItsText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		text  string
		caret buffer.Pos
		edits []LSPEdit
		want  buffer.Pos
	}{
		{"a line inserted above", "a\nb\nc\n", buffer.Pos{Line: 2, Col: 1},
			[]LSPEdit{{From: buffer.Pos{Line: 0}, To: buffer.Pos{Line: 0}, Text: "new\n"}}, buffer.Pos{Line: 3, Col: 1}},
		{"lines removed above", "a\nb\nc\nd\n", buffer.Pos{Line: 3, Col: 1},
			[]LSPEdit{{From: buffer.Pos{Line: 0}, To: buffer.Pos{Line: 2}}}, buffer.Pos{Line: 1, Col: 1}},
		{"an edit on the caret's line before it", "x := 1\n", buffer.Pos{Line: 0, Col: 6},
			[]LSPEdit{{From: buffer.Pos{Line: 0, Col: 1}, To: buffer.Pos{Line: 0, Col: 1}, Text: "yy"}}, buffer.Pos{Line: 0, Col: 8}},
		{"an edit below leaves it", "a\nb\n", buffer.Pos{Line: 0, Col: 1},
			[]LSPEdit{{From: buffer.Pos{Line: 1}, To: buffer.Pos{Line: 1, Col: 1}, Text: "zzz"}}, buffer.Pos{Line: 0, Col: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e, path := openFixture(t, Options{}, tt.text)
			d := e.Active()
			d.Cursor = tt.caret
			if err := e.ApplyLSPEdits(path, tt.edits); err != nil {
				t.Fatal(err)
			}
			if d.Cursor != tt.want {
				t.Fatalf("caret = %+v, want %+v in %q", d.Cursor, tt.want, d.Buffer.Text())
			}
		})
	}
}

// TestApplyLSPEditsKeepsTheExtraCarets checks that a format does not drop the other carets, and
// that each of them moves with its text.
func TestApplyLSPEditsKeepsTheExtraCarets(t *testing.T) {
	t.Parallel()
	e, path := openFixture(t, Options{}, "a\nb\nc\n")
	d := e.Active()
	e.Do("add_cursor_below", nil)
	if len(d.Cursors()) != 1 {
		t.Fatalf("setup: %d extra carets, want 1", len(d.Cursors()))
	}
	lines := []int{d.Cursor.Line, d.Cursors()[0].Cursor.Line}
	edit := []LSPEdit{{From: buffer.Pos{Line: 0}, To: buffer.Pos{Line: 0}, Text: "new\n"}}
	if err := e.ApplyLSPEdits(path, edit); err != nil {
		t.Fatal(err)
	}
	extra := d.Cursors()
	if len(extra) != 1 {
		t.Fatalf("extra carets = %+v after the edit, want one", extra)
	}
	if got := []int{d.Cursor.Line, extra[0].Cursor.Line}; got[0] != lines[0]+1 || got[1] != lines[1]+1 {
		t.Fatalf("caret lines = %v, want %v moved down one line", got, lines)
	}
}
