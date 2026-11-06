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
