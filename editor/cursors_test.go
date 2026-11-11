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
	Primary Caret
	extras  []Caret
}

func (c carets) String() string {
	s := fmt.Sprintf("primary %s", fmtCaret(c.Primary))
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
	return carets{Primary: primary, extras: d.Cursors()}
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
	return sameCaret(got.Primary, want.Primary) && slices.EqualFunc(got.extras, want.extras, sameCaret)
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
		Name    string
		Text    string
		Start   buffer.Pos
		action  Action
		wantErr bool
		Want    carets
	}{
		{
			Name:   "below clamps to a shorter line",
			Text:   "abcd\nef\nghij",
			Start:  caretPos(0, 2),
			action: "add_cursor_below",
			Want:   carets{Primary: Caret{Cursor: caretPos(1, 2)}, extras: []Caret{{Cursor: caretPos(0, 2)}}},
		},
		{
			Name:   "below keeps the column on a longer line",
			Text:   "abcd\nefgh\nij",
			Start:  caretPos(0, 3),
			action: "add_cursor_below",
			Want:   carets{Primary: Caret{Cursor: caretPos(1, 3)}, extras: []Caret{{Cursor: caretPos(0, 3)}}},
		},
		{
			Name:   "above keeps the column",
			Text:   "abcd\nefgh",
			Start:  caretPos(1, 4),
			action: "add_cursor_above",
			Want:   carets{Primary: Caret{Cursor: caretPos(0, 4)}, extras: []Caret{{Cursor: caretPos(1, 4)}}},
		},
		{
			Name:   "above clamps to a shorter line",
			Text:   "ab\nefgh",
			Start:  caretPos(1, 3),
			action: "add_cursor_above",
			Want:   carets{Primary: Caret{Cursor: caretPos(0, 2)}, extras: []Caret{{Cursor: caretPos(1, 3)}}},
		},
		{
			Name:   "above matches the display column past a tab",
			Text:   "\tx\nabcdef",
			Start:  caretPos(1, 4),
			action: "add_cursor_above",
			Want:   carets{Primary: Caret{Cursor: caretPos(0, 1)}, extras: []Caret{{Cursor: caretPos(1, 4)}}},
		},
		{
			Name:   "below keeps the display column past wide characters",
			Text:   "日本x\nabcdef",
			Start:  caretPos(0, 2),
			action: "add_cursor_below",
			Want:   carets{Primary: Caret{Cursor: caretPos(1, 4)}, extras: []Caret{{Cursor: caretPos(0, 2)}}},
		},
		{
			Name:    "below on the last line is refused",
			Text:    "ab\ncd",
			Start:   caretPos(1, 1),
			action:  "add_cursor_below",
			wantErr: true,
			Want:    carets{Primary: Caret{Cursor: caretPos(1, 1)}},
		},
		{
			Name:    "above on the first line is refused",
			Text:    "ab\ncd",
			Start:   caretPos(0, 1),
			action:  "add_cursor_above",
			wantErr: true,
			Want:    carets{Primary: Caret{Cursor: caretPos(0, 1)}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := New(Options{})
			typeText(t, e, tt.Text)
			e.SetCursor(tt.Start, false)
			r := e.Do(tt.action, nil)
			if (r.Err != nil) != tt.wantErr {
				t.Fatalf("Do(%s) err = %v, want error %v", tt.action, r.Err, tt.wantErr)
			}
			if got := caretState(e); !sameCarets(got, tt.Want) {
				t.Fatalf("carets = %v, want %v", got, tt.Want)
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
		Primary: Caret{Cursor: caretPos(1, 0)},
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
		Name     string
		Text     string
		setup    func(t *testing.T, e *Editor)
		wantErr  bool
		wantText string
		Want     carets
	}{
		{
			Name: "selects the word at the caret and adds no caret",
			Text: "foo bar foo",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 1), false)
			},
			Want: carets{Primary: Caret{Cursor: caretPos(0, 3), Sel: caretSel(caretPos(0, 0), caretPos(0, 3))}},
		},
		{
			Name: "selects the word before a caret placed just after it",
			Text: "foo bar",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 3), false)
			},
			Want: carets{Primary: Caret{Cursor: caretPos(0, 3), Sel: caretSel(caretPos(0, 0), caretPos(0, 3))}},
		},
		{
			Name: "refuses when there is no word at the caret",
			Text: "a, b",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 2), false)
			},
			wantErr: true,
			Want:    carets{Primary: Caret{Cursor: caretPos(0, 2)}},
		},
		{
			Name: "adds the next occurrence after the selection",
			Text: "foo bar foo baz foo",
			setup: func(t *testing.T, e *Editor) {
				e.SelectRange(caretPos(0, 0), caretPos(0, 3))
			},
			Want: carets{
				Primary: Caret{Cursor: caretPos(0, 11), Sel: caretSel(caretPos(0, 8), caretPos(0, 11))},
				extras:  []Caret{{Cursor: caretPos(0, 3), Sel: caretSel(caretPos(0, 0), caretPos(0, 3))}},
			},
		},
		{
			Name: "keeps a backward selection backward",
			Text: "foo bar foo",
			setup: func(t *testing.T, e *Editor) {
				e.SelectRange(caretPos(0, 3), caretPos(0, 0))
			},
			Want: carets{
				Primary: Caret{Cursor: caretPos(0, 8), Sel: caretSel(caretPos(0, 11), caretPos(0, 8))},
				extras:  []Caret{{Cursor: caretPos(0, 0), Sel: caretSel(caretPos(0, 3), caretPos(0, 0))}},
			},
		},
		{
			Name: "wraps to the start of the document",
			Text: "foo bar foo",
			setup: func(t *testing.T, e *Editor) {
				e.SelectRange(caretPos(0, 8), caretPos(0, 11))
			},
			Want: carets{
				Primary: Caret{Cursor: caretPos(0, 3), Sel: caretSel(caretPos(0, 0), caretPos(0, 3))},
				extras:  []Caret{{Cursor: caretPos(0, 11), Sel: caretSel(caretPos(0, 8), caretPos(0, 11))}},
			},
		},
		{
			Name: "matches a selection that spans lines",
			Text: "ab\ncd\nab\ncd",
			setup: func(t *testing.T, e *Editor) {
				e.SelectRange(caretPos(0, 0), caretPos(1, 2))
			},
			Want: carets{
				Primary: Caret{Cursor: caretPos(3, 2), Sel: caretSel(caretPos(2, 0), caretPos(3, 2))},
				extras:  []Caret{{Cursor: caretPos(1, 2), Sel: caretSel(caretPos(0, 0), caretPos(1, 2))}},
			},
		},
		{
			Name: "skips an occurrence that already has a caret",
			Text: "foo foo foo",
			setup: func(t *testing.T, e *Editor) {
				e.SelectRange(caretPos(0, 4), caretPos(0, 7))
				run(t, e, "add_cursor_next_match")
				e.SelectRange(caretPos(0, 0), caretPos(0, 3))
			},
			Want: carets{
				Primary: Caret{Cursor: caretPos(0, 11), Sel: caretSel(caretPos(0, 8), caretPos(0, 11))},
				extras: []Caret{
					{Cursor: caretPos(0, 3), Sel: caretSel(caretPos(0, 0), caretPos(0, 3))},
					{Cursor: caretPos(0, 7), Sel: caretSel(caretPos(0, 4), caretPos(0, 7))},
				},
			},
		},
		{
			Name: "counts columns in runes, not bytes",
			Text: "é foo é foo",
			setup: func(t *testing.T, e *Editor) {
				e.SelectRange(caretPos(0, 2), caretPos(0, 5))
			},
			Want: carets{
				Primary: Caret{Cursor: caretPos(0, 11), Sel: caretSel(caretPos(0, 8), caretPos(0, 11))},
				extras:  []Caret{{Cursor: caretPos(0, 5), Sel: caretSel(caretPos(0, 2), caretPos(0, 5))}},
			},
		},
		{
			Name: "refuses when every match is already selected",
			Text: "foo",
			setup: func(t *testing.T, e *Editor) {
				e.SelectRange(caretPos(0, 0), caretPos(0, 3))
			},
			wantErr: true,
			Want:    carets{Primary: Caret{Cursor: caretPos(0, 3), Sel: caretSel(caretPos(0, 0), caretPos(0, 3))}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := New(Options{})
			typeText(t, e, tt.Text)
			setCaretsAfter(t, e, tt.setup)
			r := e.Do("add_cursor_next_match", nil)
			if (r.Err != nil) != tt.wantErr {
				t.Fatalf("add_cursor_next_match err = %v, want error %v", r.Err, tt.wantErr)
			}
			if got := caretState(e); !sameCarets(got, tt.Want) {
				t.Fatalf("carets = %v, want %v", got, tt.Want)
			}
			if text(e) != tt.Text {
				t.Fatalf("text = %q, want %q", text(e), tt.Text)
			}
		})
	}
}

func TestAddCursorsToLineEnds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name  string
		Text  string
		setup func(t *testing.T, e *Editor)
		Want  carets
	}{
		{
			Name: "a selection over two lines puts a caret at each line end",
			Text: "ab\ncde\nf",
			setup: func(t *testing.T, e *Editor) {
				e.SelectRange(caretPos(0, 1), caretPos(1, 1))
			},
			Want: carets{
				Primary: Caret{Cursor: caretPos(0, 2)},
				extras:  []Caret{{Cursor: caretPos(1, 3)}},
			},
		},
		{
			Name: "a selection ending at column 0 leaves its last line out",
			Text: "ab\ncde\nf",
			setup: func(t *testing.T, e *Editor) {
				e.SelectRange(caretPos(0, 1), caretPos(2, 0))
			},
			Want: carets{
				Primary: Caret{Cursor: caretPos(0, 2)},
				extras:  []Caret{{Cursor: caretPos(1, 3)}},
			},
		},
		{
			Name: "without a selection the caret moves to its line end",
			Text: "ab\ncde\nf",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(1, 0), false)
			},
			Want: carets{Primary: Caret{Cursor: caretPos(1, 3)}},
		},
		{
			Name: "every caret contributes its own line",
			Text: "ab\ncde",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 0), false)
				run(t, e, "add_cursor_below")
			},
			Want: carets{
				Primary: Caret{Cursor: caretPos(1, 3)},
				extras:  []Caret{{Cursor: caretPos(0, 2)}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := New(Options{})
			typeText(t, e, tt.Text)
			setCaretsAfter(t, e, tt.setup)
			if r := e.Do("add_cursors_to_line_ends", nil); r.Err != nil {
				t.Fatalf("add_cursors_to_line_ends: %v", r.Err)
			}
			if got := caretState(e); !sameCarets(got, tt.Want) {
				t.Fatalf("carets = %v, want %v", got, tt.Want)
			}
		})
	}
}

func TestRemoveSecondaryCursors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name  string
		Text  string
		setup func(t *testing.T, e *Editor)
		Want  carets
	}{
		{
			Name: "clears the extra carets and the primary selection",
			Text: "ab\ncd",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 1), false)
				run(t, e, "add_cursor_below")
				e.SelectRange(caretPos(1, 0), caretPos(1, 1))
			},
			Want: carets{Primary: Caret{Cursor: caretPos(1, 1)}},
		},
		{
			Name: "is silent with one caret",
			Text: "ab",
			Want: carets{Primary: Caret{Cursor: caretPos(0, 2)}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := New(Options{})
			typeText(t, e, tt.Text)
			setCaretsAfter(t, e, tt.setup)
			if r := e.Do("remove_secondary_cursors", nil); r.Err != nil {
				t.Fatalf("remove_secondary_cursors err = %v, want nil", r.Err)
			}
			if got := caretState(e); !sameCarets(got, tt.Want) {
				t.Fatalf("carets = %v, want %v", got, tt.Want)
			}
		})
	}
}

func TestTypingAppliesAtEveryCaret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name     string
		Text     string
		setup    func(t *testing.T, e *Editor)
		wantText string
		Want     carets
	}{
		{
			Name: "inserts at every caret",
			Text: "ab\ncd\nef",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 1), false)
				run(t, e, "add_cursor_below", "add_cursor_below")
			},
			wantText: "aXb\ncXd\neXf",
			Want: carets{
				Primary: Caret{Cursor: caretPos(2, 2)},
				extras:  []Caret{{Cursor: caretPos(0, 2)}, {Cursor: caretPos(1, 2)}},
			},
		},
		{
			Name: "replaces the selection at every caret",
			Text: "abc\nabc",
			setup: func(t *testing.T, e *Editor) {
				e.SelectRange(caretPos(0, 0), caretPos(0, 2))
				run(t, e, "add_cursor_below")
				e.SelectRange(caretPos(1, 0), caretPos(1, 2))
			},
			wantText: "Xc\nXc",
			Want: carets{
				Primary: Caret{Cursor: caretPos(1, 1)},
				extras:  []Caret{{Cursor: caretPos(0, 1)}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := New(Options{})
			typeText(t, e, tt.Text)
			setCaretsAfter(t, e, tt.setup)
			typeKey(t, e, "X")
			if text(e) != tt.wantText {
				t.Fatalf("text = %q, want %q", text(e), tt.wantText)
			}
			if got := caretState(e); !sameCarets(got, tt.Want) {
				t.Fatalf("carets = %v, want %v", got, tt.Want)
			}
		})
	}
}

func TestNewlineAppliesAtEveryCaret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name     string
		opts     Options
		Text     string
		setup    func(t *testing.T, e *Editor)
		wantText string
		Want     carets
	}{
		{
			Name: "splits every caret's line",
			Text: "ab\ncd",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(1, 1), false)
				run(t, e, "add_cursor_above")
			},
			wantText: "a\nb\nc\nd",
			Want: carets{
				Primary: Caret{Cursor: caretPos(1, 0)},
				extras:  []Caret{{Cursor: caretPos(3, 0)}},
			},
		},
		{
			Name: "copies the indent of each line when auto indent is on",
			opts: Options{AutoIndent: true},
			Text: "  ab\n  cd",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(1, 4), false)
				run(t, e, "add_cursor_above")
			},
			wantText: "  ab\n  \n  cd\n  ",
			Want: carets{
				Primary: Caret{Cursor: caretPos(1, 2)},
				extras:  []Caret{{Cursor: caretPos(3, 2)}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := New(tt.opts)
			typeText(t, e, tt.Text)
			setCaretsAfter(t, e, tt.setup)
			if r := e.Do("insert_newline", nil); r.Err != nil {
				t.Fatalf("insert_newline: %v", r.Err)
			}
			if text(e) != tt.wantText {
				t.Fatalf("text = %q, want %q", text(e), tt.wantText)
			}
			if got := caretState(e); !sameCarets(got, tt.Want) {
				t.Fatalf("carets = %v, want %v", got, tt.Want)
			}
		})
	}
}

func TestBackspaceAppliesAtEveryCaret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name     string
		Text     string
		setup    func(t *testing.T, e *Editor)
		wantText string
		Want     carets
	}{
		{
			Name: "deletes before every caret",
			Text: "abc\ndef",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(1, 2), false)
				run(t, e, "add_cursor_above")
			},
			wantText: "ac\ndf",
			Want: carets{
				Primary: Caret{Cursor: caretPos(0, 1)},
				extras:  []Caret{{Cursor: caretPos(1, 1)}},
			},
		},
		{
			Name: "joins lines and merges the carets that meet",
			Text: "ab\ncd",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 2), false)
				run(t, e, "add_cursor_below")
				e.SetCursor(caretPos(1, 0), false)
			},
			wantText: "acd",
			Want:     carets{Primary: Caret{Cursor: caretPos(0, 1)}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := New(Options{})
			typeText(t, e, tt.Text)
			setCaretsAfter(t, e, tt.setup)
			if r := e.Do("delete_backward", nil); r.Err != nil {
				t.Fatalf("delete_backward: %v", r.Err)
			}
			if text(e) != tt.wantText {
				t.Fatalf("text = %q, want %q", text(e), tt.wantText)
			}
			if got := caretState(e); !sameCarets(got, tt.Want) {
				t.Fatalf("carets = %v, want %v", got, tt.Want)
			}
		})
	}
}

func TestDeleteForwardAppliesAtEveryCaret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name     string
		Text     string
		setup    func(t *testing.T, e *Editor)
		wantText string
		Want     carets
	}{
		{
			Name: "deletes after every caret",
			Text: "abc\ndef",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 1), false)
				run(t, e, "add_cursor_below")
			},
			wantText: "ac\ndf",
			Want: carets{
				Primary: Caret{Cursor: caretPos(1, 1)},
				extras:  []Caret{{Cursor: caretPos(0, 1)}},
			},
		},
		{
			Name: "joins lines and merges the carets that meet",
			Text: "ab\ncd",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 2), false)
				run(t, e, "add_cursor_below")
				e.SetCursor(caretPos(1, 0), false)
			},
			wantText: "abd",
			Want:     carets{Primary: Caret{Cursor: caretPos(0, 2)}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := New(Options{})
			typeText(t, e, tt.Text)
			setCaretsAfter(t, e, tt.setup)
			if r := e.Do("delete_forward", nil); r.Err != nil {
				t.Fatalf("delete_forward: %v", r.Err)
			}
			if text(e) != tt.wantText {
				t.Fatalf("text = %q, want %q", text(e), tt.wantText)
			}
			if got := caretState(e); !sameCarets(got, tt.Want) {
				t.Fatalf("carets = %v, want %v", got, tt.Want)
			}
		})
	}
}

func TestTabAppliesAtEveryCaret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name     string
		opts     Options
		Text     string
		setup    func(t *testing.T, e *Editor)
		wantText string
		Want     carets
	}{
		{
			Name: "inserts tab_size spaces at every caret",
			Text: "ab\ncd",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 1), false)
				run(t, e, "add_cursor_below")
			},
			wantText: "a    b\nc    d",
			Want: carets{
				Primary: Caret{Cursor: caretPos(1, 5)},
				extras:  []Caret{{Cursor: caretPos(0, 5)}},
			},
		},
		{
			Name: "inserts a tab character at every caret when UseTabs is set",
			opts: Options{UseTabs: true},
			Text: "ab\ncd",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(0, 1), false)
				run(t, e, "add_cursor_below")
			},
			wantText: "a\tb\nc\td",
			Want: carets{
				Primary: Caret{Cursor: caretPos(1, 2)},
				extras:  []Caret{{Cursor: caretPos(0, 2)}},
			},
		},
		{
			Name: "two carets on one line each insert a full indent",
			Text: "ab",
			setup: func(t *testing.T, e *Editor) {
				e.Active().SetCarets([]caret{
					{Anchor: caretPos(0, 1), Head: caretPos(0, 1), Primary: true},
					{Anchor: caretPos(0, 2), Head: caretPos(0, 2)},
				})
			},
			wantText: "a    b    ",
			Want: carets{
				Primary: Caret{Cursor: caretPos(0, 5)},
				extras:  []Caret{{Cursor: caretPos(0, 10)}},
			},
		},
		{
			Name: "indents the lines covered by any selection",
			Text: "a\nb\nc",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(caretPos(2, 0), false)
				run(t, e, "add_cursor_above")
				e.SelectRange(caretPos(0, 0), caretPos(1, 1))
			},
			wantText: "    a\n    b\nc",
			Want: carets{
				Primary: Caret{Cursor: caretPos(1, 5), Sel: caretSel(caretPos(0, 4), caretPos(1, 5))},
				extras:  []Caret{{Cursor: caretPos(2, 0)}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := New(tt.opts)
			typeText(t, e, tt.Text)
			setCaretsAfter(t, e, tt.setup)
			if r := e.Do("insert_tab", nil); r.Err != nil {
				t.Fatalf("insert_tab: %v", r.Err)
			}
			if text(e) != tt.wantText {
				t.Fatalf("text = %q, want %q", text(e), tt.wantText)
			}
			if got := caretState(e); !sameCarets(got, tt.Want) {
				t.Fatalf("carets = %v, want %v", got, tt.Want)
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
		Name string
		in   []caret
		Want []caret
	}{
		{
			Name: "disjoint carets are sorted and kept apart",
			in: []caret{
				{Anchor: caretPos(1, 0), Head: caretPos(1, 0)},
				{Anchor: caretPos(0, 5), Head: caretPos(0, 5), Primary: true},
			},
			Want: []caret{
				{Anchor: caretPos(0, 5), Head: caretPos(0, 5), Primary: true},
				{Anchor: caretPos(1, 0), Head: caretPos(1, 0)},
			},
		},
		{
			Name: "two carets at one position merge and keep the primary flag",
			in: []caret{
				{Anchor: caretPos(0, 2), Head: caretPos(0, 2)},
				{Anchor: caretPos(0, 2), Head: caretPos(0, 2), Primary: true},
			},
			Want: []caret{{Anchor: caretPos(0, 2), Head: caretPos(0, 2), Primary: true}},
		},
		{
			Name: "overlapping selections merge into their union",
			in: []caret{
				{Anchor: caretPos(0, 0), Head: caretPos(0, 3)},
				{Anchor: caretPos(0, 2), Head: caretPos(0, 5), Primary: true},
			},
			Want: []caret{{Anchor: caretPos(0, 0), Head: caretPos(0, 5), Primary: true}},
		},
		{
			Name: "a backward primary keeps its direction in the union",
			in: []caret{
				{Anchor: caretPos(0, 0), Head: caretPos(0, 3)},
				{Anchor: caretPos(0, 5), Head: caretPos(0, 2), Primary: true},
			},
			Want: []caret{{Anchor: caretPos(0, 5), Head: caretPos(0, 0), Primary: true}},
		},
		{
			Name: "selections that only touch stay apart",
			in: []caret{
				{Anchor: caretPos(0, 0), Head: caretPos(0, 2)},
				{Anchor: caretPos(0, 2), Head: caretPos(0, 4)},
			},
			Want: []caret{
				{Anchor: caretPos(0, 0), Head: caretPos(0, 2)},
				{Anchor: caretPos(0, 2), Head: caretPos(0, 4)},
			},
		},
		{
			Name: "an empty caret at the end of a selection merges with it",
			in: []caret{
				{Anchor: caretPos(0, 0), Head: caretPos(0, 2), Primary: true},
				{Anchor: caretPos(0, 2), Head: caretPos(0, 2)},
			},
			Want: []caret{{Anchor: caretPos(0, 0), Head: caretPos(0, 2), Primary: true}},
		},
		{
			Name: "a primary caret inside another selection takes the union",
			in: []caret{
				{Anchor: caretPos(0, 0), Head: caretPos(0, 4)},
				{Anchor: caretPos(0, 2), Head: caretPos(0, 2), Primary: true},
			},
			Want: []caret{{Anchor: caretPos(0, 0), Head: caretPos(0, 4), Primary: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			got := normalize(slices.Clone(tt.in))
			if !slices.Equal(got, tt.Want) {
				t.Fatalf("normalize = %+v, want %+v", got, tt.Want)
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
		Name     string
		action   func(t *testing.T, e *Editor)
		wantText string
		Want     carets
	}{
		{
			Name:     "typing at the clamped caret",
			action:   func(t *testing.T, e *Editor) { typeKey(t, e, "X") },
			wantText: "Xab\ncd\nXef",
			Want: carets{
				Primary: Caret{Cursor: caretPos(0, 1)},
				extras:  []Caret{{Cursor: caretPos(2, 1)}},
			},
		},
		{
			Name:     "backspace at the clamped caret",
			action:   func(t *testing.T, e *Editor) { run(t, e, "delete_backward") },
			wantText: "ab\ncdef",
			Want: carets{
				Primary: Caret{Cursor: caretPos(0, 0)},
				extras:  []Caret{{Cursor: caretPos(1, 2)}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := New(Options{})
			typeText(t, e, "ab\ncd\nef")
			e.SetCursor(caretPos(0, 0), false)
			e.Active().Extra = []Caret{{Cursor: caretPos(9, 0)}}
			tt.action(t, e)
			if text(e) != tt.wantText {
				t.Fatalf("text = %q, want %q", text(e), tt.wantText)
			}
			if got := caretState(e); !sameCarets(got, tt.Want) {
				t.Fatalf("carets = %v, want %v", got, tt.Want)
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
	e.Active().SetCarets([]caret{
		{Anchor: caretPos(0, 1), Head: caretPos(0, 1)},
		{Anchor: caretPos(1, 1), Head: caretPos(1, 1), Primary: true},
		{Anchor: caretPos(2, 1), Head: caretPos(2, 1)},
	})
	typeKey(t, e, "X")
	if text(e) != "aXb\ncXd\neXf" {
		t.Fatalf("typed text = %q", text(e))
	}
	run(t, e, "undo")
	if text(e) != "ab\ncd\nef" {
		t.Fatalf("undo left text %q", text(e))
	}
	if got := caretState(e).Primary.Cursor; got != caretPos(1, 1) {
		t.Fatalf("primary after undo = %v, want 1:1", got)
	}
	run(t, e, "redo")
	if text(e) != "aXb\ncXd\neXf" {
		t.Fatalf("redo left text %q", text(e))
	}
	if got := caretState(e).Primary.Cursor; got != caretPos(1, 2) {
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
	e.Active().SetCarets([]caret{
		{Anchor: caretPos(0, 1), Head: caretPos(0, 1)},
		{Anchor: caretPos(1, 1), Head: caretPos(1, 1), Primary: true},
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
	if got := caretState(e).Primary.Cursor; got != caretPos(1, 2) {
		t.Fatalf("primary after undo = %v, want 1:2 (where Y was typed)", got)
	}
}
