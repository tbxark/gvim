package editor

import (
	"errors"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// clipEditor returns an editor holding text, with the caret at the start.
func clipEditor(t *testing.T, text string) *Editor {
	t.Helper()
	e := New(Options{})
	if r := e.Do("insert_char", map[string]any{"text": text}); r.Err != nil {
		t.Fatal(r.Err)
	}
	e.SetCursor(buffer.Pos{}, false)
	return e
}

// caretsAt places the primary caret at the first position and adds a caret at each of
// the others, by selecting them one after another.
func caretsAt(t *testing.T, e *Editor, ps ...buffer.Pos) {
	t.Helper()
	d := e.Active()
	d.SetCarets(func() []caret {
		out := make([]caret, len(ps))
		for i, p := range ps {
			out[i] = caret{Anchor: p, Head: p, Primary: i == 0}
		}
		return out
	}())
}

func TestCopyCut(t *testing.T) {
	t.Parallel()
	pos := func(line, col int) buffer.Pos { return buffer.Pos{Line: line, Col: col} }
	tests := []struct {
		Name      string
		Text      string
		setup     func(t *testing.T, e *Editor)
		action    Action
		wantClip  string
		wantText  string
		wantState string
	}{
		{
			Name:     "copy without selection takes the line and its break",
			Text:     "one\ntwo\nthree",
			setup:    func(_ *testing.T, e *Editor) { e.SetCursor(pos(1, 1), false) },
			action:   "copy",
			wantClip: "two\n",
			wantText: "one\ntwo\nthree",
		},
		{
			Name:     "copy of the last line has no break",
			Text:     "one\ntwo",
			setup:    func(_ *testing.T, e *Editor) { e.SetCursor(pos(1, 0), false) },
			action:   "copy",
			wantClip: "two",
			wantText: "one\ntwo",
		},
		{
			Name:     "cut without selection removes the line",
			Text:     "one\ntwo\nthree",
			setup:    func(_ *testing.T, e *Editor) { e.SetCursor(pos(1, 1), false) },
			action:   "cut",
			wantClip: "two\n",
			wantText: "one\nthree",
		},
		{
			Name:     "cut of the last line leaves an empty line, as Fresh does",
			Text:     "one\ntwo",
			setup:    func(_ *testing.T, e *Editor) { e.SetCursor(pos(1, 0), false) },
			action:   "cut",
			wantClip: "two",
			wantText: "one\n",
		},
		{
			Name: "copy joins the selections of all carets with a newline",
			Text: "abcd\nefgh\nijkl",
			setup: func(t *testing.T, e *Editor) {
				selectCarets(t, e, [2]buffer.Pos{pos(0, 0), pos(0, 2)}, [2]buffer.Pos{pos(2, 1), pos(2, 4)})
			},
			action:   "copy",
			wantClip: "ab\njkl",
			wantText: "abcd\nefgh\nijkl",
		},
		{
			Name: "copy_with_theme joins the selections of all carets",
			Text: "abcd\nefgh\nijkl",
			setup: func(t *testing.T, e *Editor) {
				selectCarets(t, e, [2]buffer.Pos{pos(0, 0), pos(0, 2)}, [2]buffer.Pos{pos(2, 1), pos(2, 4)})
			},
			action:   "copy_with_theme",
			wantClip: "ab\njkl",
			wantText: "abcd\nefgh\nijkl",
		},
		{
			Name: "cut deletes the selections of all carets",
			Text: "abcd\nefgh\nijkl",
			setup: func(t *testing.T, e *Editor) {
				selectCarets(t, e, [2]buffer.Pos{pos(0, 0), pos(0, 2)}, [2]buffer.Pos{pos(2, 1), pos(2, 4)})
			},
			action:   "cut",
			wantClip: "ab\njkl",
			wantText: "cd\nefgh\ni",
		},
		{
			Name:     "copy without selection takes each caret line once",
			Text:     "a\nb\nc\nd",
			setup:    func(t *testing.T, e *Editor) { caretsAt(t, e, pos(0, 0), pos(2, 0), pos(2, 1)) },
			action:   "copy",
			wantClip: "a\nc\n",
			wantText: "a\nb\nc\nd",
		},
		{
			Name:     "cut without selection removes each caret line once",
			Text:     "a\nb\nc\nd",
			setup:    func(t *testing.T, e *Editor) { caretsAt(t, e, pos(0, 0), pos(2, 0), pos(2, 1)) },
			action:   "cut",
			wantClip: "a\nc\n",
			wantText: "b\nd",
		},
		{
			Name: "block copy keeps a row for a line the rectangle misses",
			Text: "abcd\nx\nijkl",
			setup: func(t *testing.T, e *Editor) {
				e.SetCursor(pos(0, 1), false)
				run(t, e, "block_select_down", "block_select_down", "block_select_right", "block_select_right")
			},
			action:   "copy",
			wantClip: "bc\n\njk",
			wantText: "abcd\nx\nijkl",
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := clipEditor(t, tt.Text)
			tt.setup(t, e)
			r := e.Do(tt.action, nil)
			if r.Err != nil {
				t.Fatalf("%s: %v", tt.action, r.Err)
			}
			if e.clipboard != tt.wantClip || r.Text != tt.wantClip {
				t.Fatalf("clipboard = %q, Result.Text = %q, want %q", e.clipboard, r.Text, tt.wantClip)
			}
			if got := text(e); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if tt.action == "cut" {
				run(t, e, "undo")
				if got := text(e); got != tt.Text {
					t.Fatalf("one undo gives %q, want %q", got, tt.Text)
				}
			}
		})
	}
}

// selectCarets makes one caret per range, the first being the primary one.
func selectCarets(t *testing.T, e *Editor, ranges ...[2]buffer.Pos) {
	t.Helper()
	cs := make([]caret, len(ranges))
	for i, r := range ranges {
		cs[i] = caret{Anchor: r[0], Head: r[1], Primary: i == 0}
	}
	e.Active().SetCarets(cs)
}

func TestCopyOfNothingFails(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	r := e.Do("copy", nil)
	if !errors.Is(r.Err, ErrNothingSelected) {
		t.Fatalf("copy err = %v, want ErrNothingSelected", r.Err)
	}
	if r.Text != "" || e.clipboard != "" {
		t.Fatalf("a failed copy handed back %q and set %q", r.Text, e.clipboard)
	}
}

func TestPaste(t *testing.T) {
	t.Parallel()
	pos := func(line, col int) buffer.Pos { return buffer.Pos{Line: line, Col: col} }
	tests := []struct {
		Name     string
		Text     string
		setup    func(t *testing.T, e *Editor)
		register string
		Args     map[string]any
		Want     string
	}{
		{
			Name:     "the private register is the default",
			Text:     "ab",
			setup:    func(_ *testing.T, e *Editor) { e.SetCursor(pos(0, 1), false) },
			register: "X",
			Want:     "aXb",
		},
		{
			Name:     "the text argument wins over the register",
			Text:     "ab",
			setup:    func(_ *testing.T, e *Editor) { e.SetCursor(pos(0, 1), false) },
			register: "X",
			Args:     map[string]any{"text": "Y"},
			Want:     "aYb",
		},
		{
			Name:  "CRLF and CR become LF",
			Text:  "ab",
			Args:  map[string]any{"text": "1\r\n2\r3\n4"},
			Want:  "1\n2\n3\n4ab",
			setup: func(_ *testing.T, _ *Editor) {},
		},
		{
			Name:  "every caret gets the whole text",
			Text:  "ab\ncd",
			setup: func(t *testing.T, e *Editor) { caretsAt(t, e, pos(0, 1), pos(1, 1)) },
			Args:  map[string]any{"text": "Q"},
			Want:  "aQb\ncQd",
		},
		{
			Name:  "as many lines as carets give one line to each caret",
			Text:  "ab\ncd",
			setup: func(t *testing.T, e *Editor) { caretsAt(t, e, pos(0, 1), pos(1, 1)) },
			Args:  map[string]any{"text": "X\nY\n"},
			Want:  "aXb\ncYd",
		},
		{
			Name:  "a different line count goes whole to every caret",
			Text:  "ab\ncd",
			setup: func(t *testing.T, e *Editor) { caretsAt(t, e, pos(0, 1), pos(1, 1)) },
			Args:  map[string]any{"text": "X\nY\nZ"},
			Want:  "aX\nY\nZb\ncX\nY\nZd",
		},
		{
			Name: "a selection is replaced",
			Text: "abcd",
			setup: func(t *testing.T, e *Editor) {
				selectCarets(t, e, [2]buffer.Pos{pos(0, 1), pos(0, 3)})
			},
			Args: map[string]any{"text": "-"},
			Want: "a-d",
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := clipEditor(t, tt.Text)
			tt.setup(t, e)
			e.clipboard = tt.register
			if r := e.Do("paste", tt.Args); r.Err != nil {
				t.Fatal(r.Err)
			}
			if got := text(e); got != tt.Want {
				t.Fatalf("text = %q, want %q", got, tt.Want)
			}
			run(t, e, "undo")
			if got := text(e); got != tt.Text {
				t.Fatalf("one undo gives %q, want %q", got, tt.Text)
			}
		})
	}
}

func TestPasteOverTheCapIsRefused(t *testing.T) {
	t.Parallel()
	e := clipEditor(t, "abc")
	big := strings.Repeat("x", buffer.MaxMaterialize+1)
	r := e.Do("paste", map[string]any{"text": big})
	if !errors.Is(r.Err, ErrPasteTooLarge) {
		t.Fatalf("err = %v, want ErrPasteTooLarge", r.Err)
	}
	if got := text(e); got != "abc" {
		t.Fatalf("text changed to %d bytes", len(got))
	}
	if got := e.Status(); !strings.Contains(got, "too large") {
		t.Fatalf("status = %q, want the refusal", got)
	}
}

func TestPasteTextIsRefusedWhenReadOnly(t *testing.T) {
	t.Parallel()
	e := clipEditor(t, "abc")
	run(t, e, "toggle_read_only")
	r := e.Do("paste", map[string]any{"text": "x"})
	if !errors.Is(r.Err, ErrReadOnly) {
		t.Fatalf("err = %v, want ErrReadOnly", r.Err)
	}
	if got := text(e); got != "abc" {
		t.Fatalf("text = %q", got)
	}
}
