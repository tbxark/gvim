package editor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

func TestWordMotions(t *testing.T) {
	t.Parallel()
	// Column positions in "foo bar.baz\n  qux" are noted in each case.
	doc := "foo bar.baz\n  qux"
	tests := []struct {
		name  string
		start buffer.Pos
		fn    func(*buffer.Buffer, buffer.Pos) buffer.Pos
		want  buffer.Pos
	}{
		{"left from end of word", buffer.Pos{Line: 0, Col: 7}, wordLeft, buffer.Pos{Line: 0, Col: 4}},
		{"left across punctuation", buffer.Pos{Line: 0, Col: 8}, wordLeft, buffer.Pos{Line: 0, Col: 7}},
		{"left skips spaces", buffer.Pos{Line: 0, Col: 4}, wordLeft, buffer.Pos{Line: 0, Col: 0}},
		{"left at start", buffer.Pos{Line: 0, Col: 0}, wordLeft, buffer.Pos{Line: 0, Col: 0}},
		{"left from line start lands on previous word", buffer.Pos{Line: 1, Col: 0}, wordLeft, buffer.Pos{Line: 0, Col: 8}},
		{"right to next word", buffer.Pos{Line: 0, Col: 0}, wordRight, buffer.Pos{Line: 0, Col: 4}},
		{"right over punctuation", buffer.Pos{Line: 0, Col: 7}, wordRight, buffer.Pos{Line: 0, Col: 8}},
		{"right crosses line", buffer.Pos{Line: 0, Col: 11}, wordRight, buffer.Pos{Line: 1, Col: 2}},
		{"right at end", buffer.Pos{Line: 1, Col: 5}, wordRight, buffer.Pos{Line: 1, Col: 5}},
		{"end from start", buffer.Pos{Line: 0, Col: 0}, wordEnd, buffer.Pos{Line: 0, Col: 3}},
		{"end from end of word", buffer.Pos{Line: 0, Col: 3}, wordEnd, buffer.Pos{Line: 0, Col: 7}},
		{"end at last word", buffer.Pos{Line: 1, Col: 2}, wordEnd, buffer.Pos{Line: 1, Col: 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := buffer.New(doc)
			if got := tt.fn(b, tt.start); got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSmartHomeTogglesBetweenIndentAndColumnZero(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	typeText(t, e, "  x")
	run(t, e, "smart_home")
	if c := e.Active().Cursor.Col; c != 2 {
		t.Fatalf("first smart_home col = %d, want 2", c)
	}
	run(t, e, "smart_home")
	if c := e.Active().Cursor.Col; c != 0 {
		t.Fatalf("second smart_home col = %d, want 0", c)
	}
}

func TestDeleteWordForwardAndSelectWord(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	typeText(t, e, "alpha beta")
	run(t, e, "move_document_start", "delete_word_forward")
	if got := text(e); got != "beta" {
		t.Fatalf("delete_word_forward = %q, want %q", got, "beta")
	}
	run(t, e, "select_word")
	if !e.Active().HasSelection() {
		t.Fatal("select_word on a word should select it")
	}
	run(t, e, "move_document_start", "select_word")
	if got := e.Active().Sel.Head; got != (buffer.Pos{Line: 0, Col: 4}) {
		t.Fatalf("select_word head = %+v", got)
	}
}

func TestPageMotionsMoveByPaneHeight(t *testing.T) {
	t.Parallel()
	e := New(Options{Rows: 4, Cols: 10})
	for range 20 {
		run(t, e, "insert_newline")
	}
	// In a 4-row pane the page delta is 1 row, and the caret stays in the scroll-off margin.
	run(t, e, "move_document_start", "move_page_down")
	if line := e.Active().Cursor.Line; line != 2 {
		t.Fatalf("page down line = %d, want 2", line)
	}
	run(t, e, "move_page_up")
	if line := e.Active().Cursor.Line; line != 1 {
		t.Fatalf("page up line = %d, want 1", line)
	}
}

func TestPageDownScrollsByPaneMinusThree(t *testing.T) {
	t.Parallel()
	e := New(Options{Rows: 27, Cols: 80})
	for range 59 {
		run(t, e, "insert_newline")
	}
	run(t, e, "move_document_start", "move_page_down")
	d := e.Active()
	if d.Cursor.Line != 27 || d.Top != 24 {
		t.Fatalf("after page down cursor=%d top=%d, want 27 and 24", d.Cursor.Line, d.Top)
	}
	run(t, e, "move_page_up")
	if d.Cursor.Line != 3 || d.Top != 0 {
		t.Fatalf("after page up cursor=%d top=%d, want 3 and 0", d.Cursor.Line, d.Top)
	}
}

func TestLinesChangedSinceSave(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := New(Options{})
	if err := e.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	d := e.Active()
	if got := d.LinesChangedSinceSave(); got != nil {
		t.Fatalf("freshly opened = %v, want nil", got)
	}
	run(t, e, "move_document_end")
	typeText(t, e, "x")
	if got := d.LinesChangedSinceSave(); len(got) != 1 || got[0] != 2 {
		t.Fatalf("after editing line 2 = %v, want [2]", got)
	}
	run(t, e, "save")
	if got := d.LinesChangedSinceSave(); got != nil {
		t.Fatalf("after save = %v, want nil", got)
	}
}
