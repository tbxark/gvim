package editor

import (
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

func TestTabInsertsTabSizeSpaces(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		text    string
		col     int
		want    string
		wantCol int
	}{
		{"empty line", "", 0, "    ", 4},
		{"two columns in", "ab", 2, "ab    ", 6},
		{"three columns in", "abc", 3, "abc    ", 7},
		{"on a stop", "abcd", 4, "abcd    ", 8},
		{"after a tab", "a\t", 2, "a\t    ", 6},
		{"inside the text", "ab", 1, "a    b", 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{TabSize: 4}, tt.text)
			e.SetCursor(buffer.Pos{Line: 0, Col: tt.col}, false)
			run(t, e, "insert_tab")
			if got := text(e); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
			if got := e.Active().Cursor; got != (buffer.Pos{Line: 0, Col: tt.wantCol}) {
				t.Fatalf("cursor = %+v, want column %d", got, tt.wantCol)
			}
		})
	}
}

func TestTabInsertsLiteralTabWhenUseTabs(t *testing.T) {
	t.Parallel()
	e := docWith(Options{UseTabs: true, TabSize: 4}, "ab")
	e.SetCursor(buffer.Pos{Line: 0, Col: 2}, false)
	run(t, e, "insert_tab")
	if got := text(e); got != "ab\t" {
		t.Fatalf("text = %q, want a literal tab", got)
	}
}

func TestTabSizeZeroMeansFour(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "ab")
	e.SetCursor(buffer.Pos{Line: 0, Col: 2}, false)
	run(t, e, "insert_tab")
	if got := text(e); got != "ab    " {
		t.Fatalf("text = %q, want four spaces", got)
	}
}

func TestTabWithSelectionIndentsSelectedLines(t *testing.T) {
	t.Parallel()
	e := docWith(Options{TabSize: 4}, "a\nb\nc")
	e.SelectRange(buffer.Pos{Line: 0, Col: 0}, buffer.Pos{Line: 2, Col: 0})
	run(t, e, "insert_tab")
	d := e.Active()
	if got := text(e); got != "    a\n    b\nc" {
		t.Fatalf("text = %q, want lines 0 and 1 indented", got)
	}
	if d.Cursor != (buffer.Pos{Line: 2, Col: 0}) {
		t.Fatalf("caret = %+v, want the end of the selection", d.Cursor)
	}
	if d.Sel == nil || d.Sel.Anchor != (buffer.Pos{Line: 0, Col: 4}) {
		t.Fatalf("selection = %+v, want its anchor moved past the new indent", d.Sel)
	}
}

func TestTabWithSelectionOnOneLineIndentsThatLine(t *testing.T) {
	t.Parallel()
	e := docWith(Options{TabSize: 4}, "a\nb")
	e.SelectRange(buffer.Pos{Line: 1, Col: 0}, buffer.Pos{Line: 1, Col: 1})
	run(t, e, "insert_tab")
	if got := text(e); got != "a\n    b" {
		t.Fatalf("text = %q, want the selected line indented", got)
	}
	if d := e.Active(); d.Sel == nil || d.Sel.Anchor != (buffer.Pos{Line: 1, Col: 4}) || d.Cursor != (buffer.Pos{Line: 1, Col: 5}) {
		t.Fatalf("selection = %+v caret = %+v, want it to still cover b", d.Sel, d.Cursor)
	}
}

func TestDedentRemovesOneIndentUnit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		text    string
		col     int
		want    string
		wantCol int
	}{
		{"four spaces", "    a", 4, "a", 0},
		{"six spaces removes four", "      a", 6, "  a", 2},
		{"two spaces removes both", "  a", 2, "a", 0},
		{"tab removes one tab", "\ta", 1, "a", 0},
		{"space before a tab removes one space", " \ta", 2, "\ta", 1},
		{"caret inside the indent moves to the line start", "      a", 2, "  a", 0},
		{"no indent changes nothing", "a", 1, "a", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{TabSize: 4}, tt.text)
			e.SetCursor(buffer.Pos{Line: 0, Col: tt.col}, false)
			run(t, e, "dedent_selection")
			if got := text(e); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
			if got := e.Active().Cursor; got != (buffer.Pos{Line: 0, Col: tt.wantCol}) {
				t.Fatalf("cursor = %+v, want column %d", got, tt.wantCol)
			}
		})
	}
}

func TestDedentSelectedLinesLeavesEndLineOut(t *testing.T) {
	t.Parallel()
	e := docWith(Options{TabSize: 4}, "    a\nb\n      c\n    d")
	e.SelectRange(buffer.Pos{Line: 0, Col: 0}, buffer.Pos{Line: 3, Col: 0})
	run(t, e, "dedent_selection")
	d := e.Active()
	if got := text(e); got != "a\nb\n  c\n    d" {
		t.Fatalf("text = %q", got)
	}
	if d.Cursor != (buffer.Pos{Line: 3, Col: 0}) || d.Sel == nil || d.Sel.Anchor != (buffer.Pos{Line: 0, Col: 0}) {
		t.Fatalf("selection = %+v caret = %+v, want it kept on the same text", d.Sel, d.Cursor)
	}
}

func TestDedentSelectionEndingInsideALineIncludesIt(t *testing.T) {
	t.Parallel()
	e := docWith(Options{TabSize: 4}, "    a\n    b")
	e.SelectRange(buffer.Pos{Line: 0, Col: 0}, buffer.Pos{Line: 1, Col: 2})
	run(t, e, "dedent_selection")
	if got := text(e); got != "a\nb" {
		t.Fatalf("text = %q, want both lines dedented", got)
	}
	if got := e.Active().Cursor; got != (buffer.Pos{Line: 1, Col: 0}) {
		t.Fatalf("caret = %+v, want column 0 on line 1", got)
	}
}

func TestDedentTabSizeZeroMeansFour(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "      a")
	e.SetCursor(buffer.Pos{Line: 0, Col: 6}, false)
	run(t, e, "dedent_selection")
	if got := text(e); got != "  a" {
		t.Fatalf("text = %q, want four columns removed", got)
	}
}

func TestAutoIndentCopiesLeadingWhitespace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		text     string
		col      int
		want     string
		wantLine int
		wantCol  int
	}{
		{"spaces", "  foo", 5, "  foo\n  ", 1, 2},
		{"tab becomes tab_size spaces without use_tabs", "\tfoo", 4, "\tfoo\n    ", 1, 4},
		{"no indent", "foo", 3, "foo\n", 1, 0},
		{"caret inside the indent copies up to the caret", "    foo", 2, "  \n    foo", 1, 2},
		{"caret at column 0 copies nothing", "  foo", 0, "\n  foo", 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{AutoIndent: true, TabSize: 4}, tt.text)
			e.SetCursor(buffer.Pos{Line: 0, Col: tt.col}, false)
			run(t, e, "insert_newline")
			if got := text(e); got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
			if got := e.Active().Cursor; got != (buffer.Pos{Line: tt.wantLine, Col: tt.wantCol}) {
				t.Fatalf("cursor = %+v, want line %d col %d", got, tt.wantLine, tt.wantCol)
			}
		})
	}
}

func TestAutoIndentOffKeepsTheLineStart(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "  foo")
	e.SetCursor(buffer.Pos{Line: 0, Col: 5}, false)
	run(t, e, "insert_newline")
	if got := text(e); got != "  foo\n" {
		t.Fatalf("text = %q, want no indent copied", got)
	}
}

func TestAutoIndentAfterReplacingSelection(t *testing.T) {
	t.Parallel()
	e := docWith(Options{AutoIndent: true, TabSize: 4}, "  foo bar")
	e.SelectRange(buffer.Pos{Line: 0, Col: 6}, buffer.Pos{Line: 0, Col: 9})
	run(t, e, "insert_newline")
	if got := text(e); got != "  foo \n  " {
		t.Fatalf("text = %q, want the selection replaced by a newline and the indent", got)
	}
	if got := e.Active().Cursor; got != (buffer.Pos{Line: 1, Col: 2}) {
		t.Fatalf("cursor = %+v, want line 1 column 2", got)
	}
}
