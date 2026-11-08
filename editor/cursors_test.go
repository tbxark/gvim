package editor

import (
	"fmt"
	"slices"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// caretPos returns the position at line and col.
func caretPos(line, col int) buffer.Pos {
	return buffer.Pos{Line: line, Col: col}
}

// caretSel returns a selection from anchor to head.
func caretSel(anchor, head buffer.Pos) *buffer.Selection {
	return &buffer.Selection{Anchor: anchor, Head: head}
}

// carets is the caret state a test expects: the primary caret and the extra
// carets in document order.
type carets struct {
	primary Caret
	extras  []Caret
}

func (c carets) String() string {
	s := fmt.Sprintf("primary %s", fmtCaret(c.primary))
	for _, x := range c.extras {
		s += ", extra " + fmtCaret(x)
	}
	return s
}

func fmtCaret(c Caret) string {
	if c.Sel == nil {
		return fmt.Sprintf("%d:%d", c.Cursor.Line, c.Cursor.Col)
	}
	return fmt.Sprintf("%d:%d sel %d:%d-%d:%d", c.Cursor.Line, c.Cursor.Col,
		c.Sel.Anchor.Line, c.Sel.Anchor.Col, c.Sel.Head.Line, c.Sel.Head.Col)
}

// caretState reads the caret state of the active document.
func caretState(e *Editor) carets {
	d := e.Active()
	primary := Caret{Cursor: d.Cursor}
	if d.HasSelection() {
		primary.Sel = d.Sel
	}
	return carets{primary: primary, extras: d.Cursors()}
}

// sameCaret reports whether a and b have the same position and selection.
func sameCaret(a, b Caret) bool {
	if a.Cursor != b.Cursor {
		return false
	}
	if a.Sel == nil || b.Sel == nil {
		return a.Sel == b.Sel
	}
	return *a.Sel == *b.Sel
}

// sameCarets reports whether got and want hold the same carets.
func sameCarets(got, want carets) bool {
	return sameCaret(got.primary, want.primary) && slices.EqualFunc(got.extras, want.extras, sameCaret)
}

// setCaretsAfter runs the setup of a table row, if it has one.
func setCaretsAfter(t *testing.T, e *Editor, setup func(*testing.T, *Editor)) {
	t.Helper()
	if setup != nil {
		setup(t, e)
	}
}

// typeKey runs insert_char with text s.
func typeKey(t *testing.T, e *Editor, s string) {
	t.Helper()
	if r := e.Do("insert_char", map[string]any{"text": s}); r.Err != nil {
		t.Fatalf("insert_char %q: %v", s, r.Err)
	}
}

func TestAddCursorAboveAndBelow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		text    string
		start   buffer.Pos
		action  Action
		wantErr bool
		want    carets
	}{
		{
			name:   "below clamps to a shorter line",
			text:   "abcd\nef\nghij",
			start:  caretPos(0, 2),
			action: "add_cursor_below",
			want:   carets{primary: Caret{Cursor: caretPos(1, 2)}, extras: []Caret{{Cursor: caretPos(0, 2)}}},
		},
		{
			name:   "below keeps the column on a longer line",
			text:   "abcd\nefgh\nij",
			start:  caretPos(0, 3),
			action: "add_cursor_below",
			want:   carets{primary: Caret{Cursor: caretPos(1, 3)}, extras: []Caret{{Cursor: caretPos(0, 3)}}},
		},
		{
			name:   "above keeps the column",
			text:   "abcd\nefgh",
			start:  caretPos(1, 4),
			action: "add_cursor_above",
			want:   carets{primary: Caret{Cursor: caretPos(0, 4)}, extras: []Caret{{Cursor: caretPos(1, 4)}}},
		},
		{
			name:   "above clamps to a shorter line",
			text:   "ab\nefgh",
			start:  caretPos(1, 3),
			action: "add_cursor_above",
			want:   carets{primary: Caret{Cursor: caretPos(0, 2)}, extras: []Caret{{Cursor: caretPos(1, 3)}}},
		},
		{
			name:   "above matches the display column past a tab",
			text:   "\tx\nabcdef",
			start:  caretPos(1, 4),
			action: "add_cursor_above",
			want:   carets{primary: Caret{Cursor: caretPos(0, 1)}, extras: []Caret{{Cursor: caretPos(1, 4)}}},
		},
		{
			name:    "below on the last line is refused",
			text:    "ab\ncd",
			start:   caretPos(1, 1),
			action:  "add_cursor_below",
			wantErr: true,
			want:    carets{primary: Caret{Cursor: caretPos(1, 1)}},
		},
		{
			name:    "above on the first line is refused",
			text:    "ab\ncd",
			start:   caretPos(0, 1),
			action:  "add_cursor_above",
			wantErr: true,
			want:    carets{primary: Caret{Cursor: caretPos(0, 1)}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := New(Options{})
			typeText(t, e, tt.text)
			e.SetCursor(tt.start, false)
			r := e.Do(tt.action, nil)
			if (r.Err != nil) != tt.wantErr {
				t.Fatalf("Do(%s) err = %v, want error %v", tt.action, r.Err, tt.wantErr)
			}
			if got := caretState(e); !sameCarets(got, tt.want) {
				t.Fatalf("carets = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtraCursorsStayInDocumentOrder(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	typeText(t, e, "a\nb\nc\nd")
	e.SetCursor(caretPos(3, 0), false)
	run(t, e, "add_cursor_above", "add_cursor_above")
	want := carets{
		primary: Caret{Cursor: caretPos(1, 0)},
		extras:  []Caret{{Cursor: caretPos(2, 0)}, {Cursor: caretPos(3, 0)}},
	}
	if got := caretState(e); !sameCarets(got, want) {
		t.Fatalf("carets = %v, want %v", got, want)
	}
}

func TestCursorsReturnsACopy(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	typeText(t, e, "ab\ncd")
	e.SetCursor(caretPos(0, 1), false)
	run(t, e, "add_cursor_below")
	got := e.Active().Cursors()
	got[0].Cursor = caretPos(9, 9)
	if c := e.Active().Cursors()[0].Cursor; c != caretPos(0, 1) {
		t.Fatalf("Cursors() shares storage with the document: extra caret moved to %v", c)
	}
}

func TestAddNextMatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		text     string
		setup    func(t *testing.T, e *Editor)
		wantErr  bool
		wantText string
		want     carets
	}{
		{
			name: "selects the word at the caret and adds no caret",
			text: "foo bar foo",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 1), false)
			},
			want: carets{primary: Caret{Cursor: caretPos(0, 3), Sel: caretSel(caretPos(0, 0), caretPos(0, 3))}},
		},
		{
			name: "selects the word before a caret placed just after it",
			text: "foo bar",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 3), false)
			},
			want: carets{primary: Caret{Cursor: caretPos(0, 3), Sel: caretSel(caretPos(0, 0), caretPos(0, 3))}},
		},
		{
			name: "refuses when there is no word at the caret",
			text: "a, b",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 2), false)
			},
			wantErr: true,
			want:    carets{primary: Caret{Cursor: caretPos(0, 2)}},
		},
		{
			name: "adds the next occurrence after the selection",
			text: "foo bar foo baz foo",
			setup: func(t *testing.T, e *Editor) {
				e.SelectRange(caretPos(0, 0), caretPos(0, 3))
			},
			want: carets{
				primary: Caret{Cursor: caretPos(0, 11), Sel: caretSel(caretPos(0, 8), caretPos(0, 11))},
				extras:  []Caret{{Cursor: caretPos(0, 3), Sel: caretSel(caretPos(0, 0), caretPos(0, 3))}},
			},
		},
		{
			name: "keeps a backward selection backward",
			text: "foo bar foo",
			setup: func(t *testing.T, e *Editor) {
				e.SelectRange(caretPos(0, 3), caretPos(0, 0))
			},
			want: carets{
				primary: Caret{Cursor: caretPos(0, 8), Sel: caretSel(caretPos(0, 11), caretPos(0, 8))},
				extras:  []Caret{{Cursor: caretPos(0, 0), Sel: caretSel(caretPos(0, 3), caretPos(0, 0))}},
			},
		},
		{
			name: "wraps to the start of the document",
			text: "foo bar foo",
			setup: func(t *testing.T, e *Editor) {
				e.SelectRange(caretPos(0, 8), caretPos(0, 11))
			},
			want: carets{
				primary: Caret{Cursor: caretPos(0, 3), Sel: caretSel(caretPos(0, 0), caretPos(0, 3))},
				extras:  []Caret{{Cursor: caretPos(0, 11), Sel: caretSel(caretPos(0, 8), caretPos(0, 11))}},
			},
		},
		{
			name: "matches a selection that spans lines",
			text: "ab\ncd\nab\ncd",
			setup: func(t *testing.T, e *Editor) {
				e.SelectRange(caretPos(0, 0), caretPos(1, 2))
			},
			want: carets{
				primary: Caret{Cursor: caretPos(3, 2), Sel: caretSel(caretPos(2, 0), caretPos(3, 2))},
				extras:  []Caret{{Cursor: caretPos(1, 2), Sel: caretSel(caretPos(0, 0), caretPos(1, 2))}},
			},
		},
		{
			name: "skips an occurrence that already has a caret",
			text: "foo foo foo",
			setup: func(t *testing.T, e *Editor) {
				e.SelectRange(caretPos(0, 4), caretPos(0, 7))
				run(t, e, "add_cursor_next_match")
				e.SelectRange(caretPos(0, 0), caretPos(0, 3))
			},
			want: carets{
				primary: Caret{Cursor: caretPos(0, 11), Sel: caretSel(caretPos(0, 8), caretPos(0, 11))},
				extras: []Caret{
					{Cursor: caretPos(0, 3), Sel: caretSel(caretPos(0, 0), caretPos(0, 3))},
					{Cursor: caretPos(0, 7), Sel: caretSel(caretPos(0, 4), caretPos(0, 7))},
				},
			},
		},
		{
			name: "counts columns in runes, not bytes",
			text: "é foo é foo",
			setup: func(t *testing.T, e *Editor) {
				e.SelectRange(caretPos(0, 2), caretPos(0, 5))
			},
			want: carets{
				primary: Caret{Cursor: caretPos(0, 11), Sel: caretSel(caretPos(0, 8), caretPos(0, 11))},
				extras:  []Caret{{Cursor: caretPos(0, 5), Sel: caretSel(caretPos(0, 2), caretPos(0, 5))}},
			},
		},
		{
			name: "refuses when every match is already selected",
			text: "foo",
			setup: func(t *testing.T, e *Editor) {
				e.SelectRange(caretPos(0, 0), caretPos(0, 3))
			},
			wantErr: true,
			want:    carets{primary: Caret{Cursor: caretPos(0, 3), Sel: caretSel(caretPos(0, 0), caretPos(0, 3))}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := New(Options{})
			typeText(t, e, tt.text)
			setCaretsAfter(t, e, tt.setup)
			r := e.Do("add_cursor_next_match", nil)
			if (r.Err != nil) != tt.wantErr {
				t.Fatalf("add_cursor_next_match err = %v, want error %v", r.Err, tt.wantErr)
			}
			if got := caretState(e); !sameCarets(got, tt.want) {
				t.Fatalf("carets = %v, want %v", got, tt.want)
			}
			if text(e) != tt.text {
				t.Fatalf("text = %q, want %q", text(e), tt.text)
			}
		})
	}
}

func TestAddCursorsToLineEnds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		text  string
		setup func(t *testing.T, e *Editor)
		want  carets
	}{
		{
			name: "a selection over two lines puts a caret at each line end",
			text: "ab\ncde\nf",
			setup: func(t *testing.T, e *Editor) {
				e.SelectRange(caretPos(0, 1), caretPos(1, 1))
			},
			want: carets{
				primary: Caret{Cursor: caretPos(0, 2)},
				extras:  []Caret{{Cursor: caretPos(1, 3)}},
			},
		},
		{
			name: "a selection ending at column 0 leaves its last line out",
			text: "ab\ncde\nf",
			setup: func(t *testing.T, e *Editor) {
				e.SelectRange(caretPos(0, 1), caretPos(2, 0))
			},
			want: carets{
				primary: Caret{Cursor: caretPos(0, 2)},
				extras:  []Caret{{Cursor: caretPos(1, 3)}},
			},
		},
		{
			name: "without a selection the caret moves to its line end",
			text: "ab\ncde\nf",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(1, 0), false)
			},
			want: carets{primary: Caret{Cursor: caretPos(1, 3)}},
		},
		{
			name: "every caret contributes its own line",
			text: "ab\ncde",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 0), false)
				run(t, e, "add_cursor_below")
			},
			want: carets{
				primary: Caret{Cursor: caretPos(1, 3)},
				extras:  []Caret{{Cursor: caretPos(0, 2)}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := New(Options{})
			typeText(t, e, tt.text)
			setCaretsAfter(t, e, tt.setup)
			if r := e.Do("add_cursors_to_line_ends", nil); r.Err != nil {
				t.Fatalf("add_cursors_to_line_ends: %v", r.Err)
			}
			if got := caretState(e); !sameCarets(got, tt.want) {
				t.Fatalf("carets = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRemoveSecondaryCursors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		text  string
		setup func(t *testing.T, e *Editor)
		want  carets
	}{
		{
			name: "clears the extra carets and the primary selection",
			text: "ab\ncd",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 1), false)
				run(t, e, "add_cursor_below")
				e.SelectRange(caretPos(1, 0), caretPos(1, 1))
			},
			want: carets{primary: Caret{Cursor: caretPos(1, 1)}},
		},
		{
			name: "is silent with one caret",
			text: "ab",
			want: carets{primary: Caret{Cursor: caretPos(0, 2)}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := New(Options{})
			typeText(t, e, tt.text)
			setCaretsAfter(t, e, tt.setup)
			if r := e.Do("remove_secondary_cursors", nil); r.Err != nil {
				t.Fatalf("remove_secondary_cursors err = %v, want nil", r.Err)
			}
			if got := caretState(e); !sameCarets(got, tt.want) {
				t.Fatalf("carets = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTypingAppliesAtEveryCaret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		text     string
		setup    func(t *testing.T, e *Editor)
		wantText string
		want     carets
	}{
		{
			name: "inserts at every caret",
			text: "ab\ncd\nef",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 1), false)
				run(t, e, "add_cursor_below", "add_cursor_below")
			},
			wantText: "aXb\ncXd\neXf",
			want: carets{
				primary: Caret{Cursor: caretPos(2, 2)},
				extras:  []Caret{{Cursor: caretPos(0, 2)}, {Cursor: caretPos(1, 2)}},
			},
		},
		{
			name: "replaces the selection at every caret",
			text: "abc\nabc",
			setup: func(t *testing.T, e *Editor) {
				e.SelectRange(caretPos(0, 0), caretPos(0, 2))
				run(t, e, "add_cursor_below")
				e.SelectRange(caretPos(1, 0), caretPos(1, 2))
			},
			wantText: "Xc\nXc",
			want: carets{
				primary: Caret{Cursor: caretPos(1, 1)},
				extras:  []Caret{{Cursor: caretPos(0, 1)}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := New(Options{})
			typeText(t, e, tt.text)
			setCaretsAfter(t, e, tt.setup)
			typeKey(t, e, "X")
			if text(e) != tt.wantText {
				t.Fatalf("text = %q, want %q", text(e), tt.wantText)
			}
			if got := caretState(e); !sameCarets(got, tt.want) {
				t.Fatalf("carets = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewlineAppliesAtEveryCaret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		opts     Options
		text     string
		setup    func(t *testing.T, e *Editor)
		wantText string
		want     carets
	}{
		{
			name: "splits every caret's line",
			text: "ab\ncd",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(1, 1), false)
				run(t, e, "add_cursor_above")
			},
			wantText: "a\nb\nc\nd",
			want: carets{
				primary: Caret{Cursor: caretPos(1, 0)},
				extras:  []Caret{{Cursor: caretPos(3, 0)}},
			},
		},
		{
			name: "copies the indent of each line when auto indent is on",
			opts: Options{AutoIndent: true},
			text: "  ab\n  cd",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(1, 4), false)
				run(t, e, "add_cursor_above")
			},
			wantText: "  ab\n  \n  cd\n  ",
			want: carets{
				primary: Caret{Cursor: caretPos(1, 2)},
				extras:  []Caret{{Cursor: caretPos(3, 2)}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := New(tt.opts)
			typeText(t, e, tt.text)
			setCaretsAfter(t, e, tt.setup)
			if r := e.Do("insert_newline", nil); r.Err != nil {
				t.Fatalf("insert_newline: %v", r.Err)
			}
			if text(e) != tt.wantText {
				t.Fatalf("text = %q, want %q", text(e), tt.wantText)
			}
			if got := caretState(e); !sameCarets(got, tt.want) {
				t.Fatalf("carets = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBackspaceAppliesAtEveryCaret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		text     string
		setup    func(t *testing.T, e *Editor)
		wantText string
		want     carets
	}{
		{
			name: "deletes before every caret",
			text: "abc\ndef",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(1, 2), false)
				run(t, e, "add_cursor_above")
			},
			wantText: "ac\ndf",
			want: carets{
				primary: Caret{Cursor: caretPos(0, 1)},
				extras:  []Caret{{Cursor: caretPos(1, 1)}},
			},
		},
		{
			name: "joins lines and merges the carets that meet",
			text: "ab\ncd",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 2), false)
				run(t, e, "add_cursor_below")
				e.SetCursor(caretPos(1, 0), false)
			},
			wantText: "acd",
			want:     carets{primary: Caret{Cursor: caretPos(0, 1)}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := New(Options{})
			typeText(t, e, tt.text)
			setCaretsAfter(t, e, tt.setup)
			if r := e.Do("delete_backward", nil); r.Err != nil {
				t.Fatalf("delete_backward: %v", r.Err)
			}
			if text(e) != tt.wantText {
				t.Fatalf("text = %q, want %q", text(e), tt.wantText)
			}
			if got := caretState(e); !sameCarets(got, tt.want) {
				t.Fatalf("carets = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeleteForwardAppliesAtEveryCaret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		text     string
		setup    func(t *testing.T, e *Editor)
		wantText string
		want     carets
	}{
		{
			name: "deletes after every caret",
			text: "abc\ndef",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 1), false)
				run(t, e, "add_cursor_below")
			},
			wantText: "ac\ndf",
			want: carets{
				primary: Caret{Cursor: caretPos(1, 1)},
				extras:  []Caret{{Cursor: caretPos(0, 1)}},
			},
		},
		{
			name: "joins lines and merges the carets that meet",
			text: "ab\ncd",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 2), false)
				run(t, e, "add_cursor_below")
				e.SetCursor(caretPos(1, 0), false)
			},
			wantText: "abd",
			want:     carets{primary: Caret{Cursor: caretPos(0, 2)}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := New(Options{})
			typeText(t, e, tt.text)
			setCaretsAfter(t, e, tt.setup)
			if r := e.Do("delete_forward", nil); r.Err != nil {
				t.Fatalf("delete_forward: %v", r.Err)
			}
			if text(e) != tt.wantText {
				t.Fatalf("text = %q, want %q", text(e), tt.wantText)
			}
			if got := caretState(e); !sameCarets(got, tt.want) {
				t.Fatalf("carets = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTabAppliesAtEveryCaret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		opts     Options
		text     string
		setup    func(t *testing.T, e *Editor)
		wantText string
		want     carets
	}{
		{
			name: "inserts tab_size spaces at every caret",
			text: "ab\ncd",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 1), false)
				run(t, e, "add_cursor_below")
			},
			wantText: "a    b\nc    d",
			want: carets{
				primary: Caret{Cursor: caretPos(1, 5)},
				extras:  []Caret{{Cursor: caretPos(0, 5)}},
			},
		},
		{
			name: "inserts a tab character at every caret when UseTabs is set",
			opts: Options{UseTabs: true},
			text: "ab\ncd",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 1), false)
				run(t, e, "add_cursor_below")
			},
			wantText: "a\tb\nc\td",
			want: carets{
				primary: Caret{Cursor: caretPos(1, 2)},
				extras:  []Caret{{Cursor: caretPos(0, 2)}},
			},
		},
		{
			name: "two carets on one line each insert a full indent",
			text: "ab",
			setup: func(t *testing.T, e *Editor) {
				e.Active().setCarets([]caret{
					{anchor: caretPos(0, 1), head: caretPos(0, 1), primary: true},
					{anchor: caretPos(0, 2), head: caretPos(0, 2)},
				})
			},
			wantText: "a    b    ",
			want: carets{
				primary: Caret{Cursor: caretPos(0, 5)},
				extras:  []Caret{{Cursor: caretPos(0, 10)}},
			},
		},
		{
			name: "indents the lines covered by any selection",
			text: "a\nb\nc",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(2, 0), false)
				run(t, e, "add_cursor_above")
				e.SelectRange(caretPos(0, 0), caretPos(1, 1))
			},
			wantText: "    a\n    b\nc",
			want: carets{
				primary: Caret{Cursor: caretPos(1, 5), Sel: caretSel(caretPos(0, 4), caretPos(1, 5))},
				extras:  []Caret{{Cursor: caretPos(2, 0)}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := New(tt.opts)
			typeText(t, e, tt.text)
			setCaretsAfter(t, e, tt.setup)
			if r := e.Do("insert_tab", nil); r.Err != nil {
				t.Fatalf("insert_tab: %v", r.Err)
			}
			if text(e) != tt.wantText {
				t.Fatalf("text = %q, want %q", text(e), tt.wantText)
			}
			if got := caretState(e); !sameCarets(got, tt.want) {
				t.Fatalf("carets = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUndoRevertsMultiCaretTypingInOneStep(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	typeText(t, e, "ab\ncd\nef")
	e.SetCursor(caretPos(0, 1), false)
	run(t, e, "add_cursor_below", "add_cursor_below")
	typeKey(t, e, "X")
	if text(e) != "aXb\ncXd\neXf" {
		t.Fatalf("typed text = %q", text(e))
	}
	run(t, e, "undo")
	if text(e) != "ab\ncd\nef" {
		t.Fatalf("one undo left text %q, want the text before typing", text(e))
	}
	if got := caretState(e); len(got.extras) != 0 {
		t.Fatalf("undo kept extra carets: %v", got)
	}
}

func TestNormalizeCarets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   []caret
		want []caret
	}{
		{
			name: "disjoint carets are sorted and kept apart",
			in: []caret{
				{anchor: caretPos(1, 0), head: caretPos(1, 0)},
				{anchor: caretPos(0, 5), head: caretPos(0, 5), primary: true},
			},
			want: []caret{
				{anchor: caretPos(0, 5), head: caretPos(0, 5), primary: true},
				{anchor: caretPos(1, 0), head: caretPos(1, 0)},
			},
		},
		{
			name: "two carets at one position merge and keep the primary flag",
			in: []caret{
				{anchor: caretPos(0, 2), head: caretPos(0, 2)},
				{anchor: caretPos(0, 2), head: caretPos(0, 2), primary: true},
			},
			want: []caret{{anchor: caretPos(0, 2), head: caretPos(0, 2), primary: true}},
		},
		{
			name: "overlapping selections merge into their union",
			in: []caret{
				{anchor: caretPos(0, 0), head: caretPos(0, 3)},
				{anchor: caretPos(0, 2), head: caretPos(0, 5), primary: true},
			},
			want: []caret{{anchor: caretPos(0, 0), head: caretPos(0, 5), primary: true}},
		},
		{
			name: "a backward primary keeps its direction in the union",
			in: []caret{
				{anchor: caretPos(0, 0), head: caretPos(0, 3)},
				{anchor: caretPos(0, 5), head: caretPos(0, 2), primary: true},
			},
			want: []caret{{anchor: caretPos(0, 5), head: caretPos(0, 0), primary: true}},
		},
		{
			name: "selections that only touch stay apart",
			in: []caret{
				{anchor: caretPos(0, 0), head: caretPos(0, 2)},
				{anchor: caretPos(0, 2), head: caretPos(0, 4)},
			},
			want: []caret{
				{anchor: caretPos(0, 0), head: caretPos(0, 2)},
				{anchor: caretPos(0, 2), head: caretPos(0, 4)},
			},
		},
		{
			name: "an empty caret at the end of a selection merges with it",
			in: []caret{
				{anchor: caretPos(0, 0), head: caretPos(0, 2), primary: true},
				{anchor: caretPos(0, 2), head: caretPos(0, 2)},
			},
			want: []caret{{anchor: caretPos(0, 0), head: caretPos(0, 2), primary: true}},
		},
		{
			name: "a primary caret inside another selection takes the union",
			in: []caret{
				{anchor: caretPos(0, 0), head: caretPos(0, 4)},
				{anchor: caretPos(0, 2), head: caretPos(0, 2), primary: true},
			},
			want: []caret{{anchor: caretPos(0, 0), head: caretPos(0, 4), primary: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalize(slices.Clone(tt.in))
			if !slices.Equal(got, tt.want) {
				t.Fatalf("normalize = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestStaleExtraCaretPastTheEndIsClampedBeforeEditing(t *testing.T) {
	t.Parallel()
	// The extra caret is past the last line, so it is clamped to 2:0 before any
	// edit uses it. Backspace there removes the line break before the last line.
	// Typing there inserts before "ef".
	tests := []struct {
		name     string
		action   func(t *testing.T, e *Editor)
		wantText string
		want     carets
	}{
		{
			name:     "typing at the clamped caret",
			action:   func(t *testing.T, e *Editor) { typeKey(t, e, "X") },
			wantText: "Xab\ncd\nXef",
			want: carets{
				primary: Caret{Cursor: caretPos(0, 1)},
				extras:  []Caret{{Cursor: caretPos(2, 1)}},
			},
		},
		{
			name:     "backspace at the clamped caret",
			action:   func(t *testing.T, e *Editor) { run(t, e, "delete_backward") },
			wantText: "ab\ncdef",
			want: carets{
				primary: Caret{Cursor: caretPos(0, 0)},
				extras:  []Caret{{Cursor: caretPos(1, 2)}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := New(Options{})
			typeText(t, e, "ab\ncd\nef")
			e.SetCursor(caretPos(0, 0), false)
			e.Active().extra = []Caret{{Cursor: caretPos(9, 0)}}
			tt.action(t, e)
			if text(e) != tt.wantText {
				t.Fatalf("text = %q, want %q", text(e), tt.wantText)
			}
			if got := caretState(e); !sameCarets(got, tt.want) {
				t.Fatalf("carets = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUndoAndRedoPutThePrimaryCaretBackOnItsOwnEdit(t *testing.T) {
	t.Parallel()
	// The primary caret is in the middle. The first recorded edit belongs to the
	// caret furthest down, so the primary must come from the recorded state.
	e := New(Options{})
	typeText(t, e, "ab\ncd\nef")
	e.Active().setCarets([]caret{
		{anchor: caretPos(0, 1), head: caretPos(0, 1)},
		{anchor: caretPos(1, 1), head: caretPos(1, 1), primary: true},
		{anchor: caretPos(2, 1), head: caretPos(2, 1)},
	})
	typeKey(t, e, "X")
	if text(e) != "aXb\ncXd\neXf" {
		t.Fatalf("typed text = %q", text(e))
	}
	run(t, e, "undo")
	if text(e) != "ab\ncd\nef" {
		t.Fatalf("undo left text %q", text(e))
	}
	if got := caretState(e).primary.Cursor; got != caretPos(1, 1) {
		t.Fatalf("primary after undo = %v, want 1:1", got)
	}
	run(t, e, "redo")
	if text(e) != "aXb\ncXd\neXf" {
		t.Fatalf("redo left text %q", text(e))
	}
	if got := caretState(e).primary.Cursor; got != caretPos(1, 2) {
		t.Fatalf("primary after redo = %v, want 1:2", got)
	}
}

func TestSingleCaretEditAfterAMultiCaretEditEndsItsUndoRecord(t *testing.T) {
	t.Parallel()
	// After the multi-caret edit, one single-caret edit makes its own undo step.
	// Undo must return to that step's place, not to the primary caret of the
	// multi-caret edit, because the record of that edit no longer matches.
	e := New(Options{})
	typeText(t, e, "ab\ncd\nef")
	e.Active().setCarets([]caret{
		{anchor: caretPos(0, 1), head: caretPos(0, 1)},
		{anchor: caretPos(1, 1), head: caretPos(1, 1), primary: true},
	})
	typeKey(t, e, "X")
	run(t, e, "remove_secondary_cursors")
	typeKey(t, e, "Y")
	if text(e) != "aXb\ncXYd\nef" {
		t.Fatalf("typed text = %q", text(e))
	}
	run(t, e, "undo")
	if text(e) != "aXb\ncXd\nef" {
		t.Fatalf("undo left text %q", text(e))
	}
	if got := caretState(e).primary.Cursor; got != caretPos(1, 2) {
		t.Fatalf("primary after undo = %v, want 1:2 (where Y was typed)", got)
	}
}
