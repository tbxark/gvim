package editor

import (
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

// multiDoc returns an editor holding text with the given carets; the first one is
// the primary caret.
func multiDoc(text string, cs ...caret) *Editor {
	e := docWith(Options{TabSize: 4}, text)
	cs[0].primary = true
	e.Active().setCarets(cs)
	return e
}

// at returns an empty caret at line:col.
func at(line, col int) caret {
	p := caretPos(line, col)
	return caret{anchor: p, head: p}
}

// span returns a caret that selects from a to b, with its head at b.
func span(a, b buffer.Pos) caret { return caret{anchor: a, head: b} }

// wantCarets builds the expected caret state from the primary caret and the extras.
func wantCarets(primary Caret, extras ...Caret) carets {
	return carets{primary: primary, extras: extras}
}

func plain(line, col int) Caret { return Caret{Cursor: caretPos(line, col)} }

func TestDeleteLineAtEveryCaret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		text     string
		carets   []caret
		wantText string
		want     carets
	}{
		{"two lines", "a\nb\nc\nd", []caret{at(0, 1), at(2, 0)}, "b\nd", wantCarets(plain(0, 0), plain(1, 0))},
		{"the last line keeps its break", "a\nb\nc\nd", []caret{at(1, 0), at(3, 0)}, "a\nc\n", wantCarets(plain(1, 0), plain(2, 0))},
		{"adjacent lines", "a\nb\nc\nd", []caret{at(1, 0), at(2, 1)}, "a\nd", wantCarets(plain(1, 0))},
		{"two carets on one line", "a\nb\nc", []caret{at(1, 0), at(1, 1)}, "a\nc", wantCarets(plain(1, 0))},
		{"a selection is dropped and not deleted", "ab\ncd\nef", []caret{span(caretPos(0, 0), caretPos(0, 2)), at(2, 1)}, "cd\n", wantCarets(plain(0, 0), plain(1, 0))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := multiDoc(tt.text, tt.carets...)
			run(t, e, "delete_line")
			if got := text(e); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if got := caretState(e); !sameCarets(got, tt.want) {
				t.Fatalf("carets = %s, want %s", got, tt.want)
			}
			run(t, e, "undo")
			if got := text(e); got != tt.text {
				t.Fatalf("text after one undo = %q, want %q", got, tt.text)
			}
		})
	}
}

func TestDeleteLineKeepsThePrimaryCaret(t *testing.T) {
	t.Parallel()
	e := multiDoc("a\nb\nc", at(2, 0), at(0, 0))
	run(t, e, "delete_line")
	if got := text(e); got != "b\n" {
		t.Fatalf("text = %q", got)
	}
	want := wantCarets(plain(1, 0), plain(0, 0))
	if got := caretState(e); !sameCarets(got, want) {
		t.Fatalf("carets = %s, want %s", got, want)
	}
}

func TestDuplicateLineAtEveryCaret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		action   Action
		text     string
		carets   []caret
		wantText string
		want     carets
	}{
		{"below", "duplicate_line", "a\nb\nc\nd", []caret{at(0, 1), at(2, 0)}, "a\na\nb\nc\nc\nd", wantCarets(plain(1, 1), plain(4, 0))},
		{"above", "duplicate_line_above", "a\nb\nc\nd", []caret{at(0, 1), at(2, 0)}, "a\na\nb\nc\nc\nd", wantCarets(plain(0, 1), plain(3, 0))},
		{"the last line", "duplicate_line", "a\nb", []caret{at(0, 0), at(1, 0)}, "a\na\nb\nb", wantCarets(plain(1, 0), plain(3, 0))},
		{"carets on one line copy it once", "duplicate_line", "a\nb", []caret{at(0, 0), at(0, 1)}, "a\na\nb", wantCarets(plain(1, 0), plain(1, 1))},
		{
			"selections",
			"duplicate_line",
			"a\nb\nc\nd",
			[]caret{span(caretPos(0, 0), caretPos(1, 1)), span(caretPos(3, 0), caretPos(3, 1))},
			"a\nb\na\nb\nc\nd\nd",
			wantCarets(
				Caret{Cursor: caretPos(3, 1), Sel: caretSel(caretPos(2, 0), caretPos(3, 1))},
				Caret{Cursor: caretPos(6, 1), Sel: caretSel(caretPos(6, 0), caretPos(6, 1))},
			),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := multiDoc(tt.text, tt.carets...)
			run(t, e, tt.action)
			if got := text(e); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if got := caretState(e); !sameCarets(got, tt.want) {
				t.Fatalf("carets = %s, want %s", got, tt.want)
			}
			run(t, e, "undo")
			if got := text(e); got != tt.text {
				t.Fatalf("text after one undo = %q, want %q", got, tt.text)
			}
		})
	}
}

func TestMoveLines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		action   Action
		text     string
		carets   []caret
		wantText string
		want     carets
	}{
		{"up", "move_line_up", "a\nb\nc", []caret{at(2, 1)}, "a\nc\nb", wantCarets(plain(1, 1))},
		{"down", "move_line_down", "a\nb\nc", []caret{at(0, 1)}, "b\na\nc", wantCarets(plain(1, 1))},
		{"the top line cannot go up", "move_line_up", "a\nb", []caret{at(0, 0)}, "a\nb", wantCarets(plain(0, 0))},
		{"the last line cannot go down", "move_line_down", "a\nb", []caret{at(1, 0)}, "a\nb", wantCarets(plain(1, 0))},
		{"down stops above the final empty line", "move_line_down", "a\nb\n", []caret{at(1, 0)}, "a\nb\n", wantCarets(plain(1, 0))},
		{"down onto the last text line", "move_line_down", "a\nb\n", []caret{at(0, 0)}, "b\na\n", wantCarets(plain(1, 0))},
		{
			"a selection moves whole and stays selected",
			"move_line_up",
			"a\nb\nc\nd",
			[]caret{span(caretPos(1, 0), caretPos(2, 1))},
			"b\nc\na\nd",
			wantCarets(Caret{Cursor: caretPos(1, 1), Sel: caretSel(caretPos(0, 0), caretPos(1, 1))}),
		},
		{
			"a reversed selection keeps its direction",
			"move_line_down",
			"a\nb\nc\nd",
			[]caret{span(caretPos(1, 1), caretPos(0, 0))},
			"c\na\nb\nd",
			wantCarets(Caret{Cursor: caretPos(1, 0), Sel: caretSel(caretPos(2, 1), caretPos(1, 0))}),
		},
		{
			"a selection that ends at column 0 leaves that line",
			"move_line_down",
			"a\nb\nc\nd",
			[]caret{span(caretPos(0, 1), caretPos(2, 0))},
			"c\na\nb\nd",
			wantCarets(Caret{Cursor: caretPos(3, 0), Sel: caretSel(caretPos(1, 1), caretPos(3, 0))}),
		},
		{"carets move apart", "move_line_up", "a\nb\nc\nd\ne", []caret{at(1, 0), at(3, 0)}, "b\na\nd\nc\ne", wantCarets(plain(0, 0), plain(2, 0))},
		{"carets move down apart", "move_line_down", "a\nb\nc\nd\ne", []caret{at(1, 0), at(3, 0)}, "a\nc\nb\ne\nd", wantCarets(plain(2, 0), plain(4, 0))},
		{"touching carets move as one block", "move_line_up", "a\nb\nc\nd\ne", []caret{at(1, 0), at(2, 0)}, "b\nc\na\nd\ne", wantCarets(plain(0, 0), plain(1, 0))},
		{"a stuck block does not hold back the others", "move_line_up", "a\nb\nc\nd", []caret{at(0, 0), at(2, 0)}, "a\nc\nb\nd", wantCarets(plain(0, 0), plain(1, 0))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := multiDoc(tt.text, tt.carets...)
			run(t, e, tt.action)
			if got := text(e); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if got := caretState(e); !sameCarets(got, tt.want) {
				t.Fatalf("carets = %s, want %s", got, tt.want)
			}
			run(t, e, "undo")
			if got := text(e); got != tt.text {
				t.Fatalf("text after one undo = %q, want %q", got, tt.text)
			}
		})
	}
}

func TestMoveLineIsOneUndoStepAcrossABucketBoundary(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "a\nb\nc")
	e.SetCursor(caretPos(1, 0), false)
	run(t, e, "move_line_down")
	if got := text(e); got != "a\nc\nb" {
		t.Fatalf("text = %q", got)
	}
	run(t, e, "undo")
	if got := text(e); got != "a\nb\nc" {
		t.Fatalf("text after one undo = %q, want the original", got)
	}
	run(t, e, "redo")
	if got := text(e); got != "a\nc\nb" {
		t.Fatalf("text after redo = %q, want the move again", got)
	}
}

func TestMoveLinesKeepsAnActiveSelection(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "a\nb\nc\nd")
	e.SelectRange(caretPos(1, 0), caretPos(2, 1))
	run(t, e, "move_line_down")
	d := e.Active()
	if got := text(e); got != "a\nd\nb\nc" {
		t.Fatalf("text = %q", got)
	}
	if !d.HasSelection() || d.Sel.Anchor != caretPos(2, 0) || d.Cursor != caretPos(3, 1) {
		t.Fatalf("selection = %+v cursor = %+v, want 2:0 to 3:1", d.Sel, d.Cursor)
	}
}

func TestDedentAtEveryCaret(t *testing.T) {
	t.Parallel()
	e := multiDoc("    a\n    b\n  c\nd", at(0, 5), at(1, 4), span(caretPos(2, 0), caretPos(2, 2)))
	run(t, e, "dedent_selection")
	if got := text(e); got != "a\nb\nc\nd" {
		t.Fatalf("text = %q", got)
	}
	want := wantCarets(plain(0, 1), plain(1, 0), Caret{Cursor: caretPos(2, 0)})
	if got := caretState(e); !sameCarets(got, want) {
		t.Fatalf("carets = %s, want %s", got, want)
	}
	run(t, e, "undo")
	if got := text(e); got != "    a\n    b\n  c\nd" {
		t.Fatalf("text after one undo = %q", got)
	}
}

func TestDedentWithNothingToRemoveChangesNothing(t *testing.T) {
	t.Parallel()
	e := multiDoc("a\nb", at(0, 1), at(1, 0))
	run(t, e, "dedent_selection")
	if got := text(e); got != "a\nb" {
		t.Fatalf("text = %q", got)
	}
	if e.Active().Buffer.Modified() {
		t.Fatal("a dedent that removes nothing must not modify the buffer")
	}
}

func TestCaseConversionAtEveryCaret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		action   Action
		text     string
		carets   []caret
		wantText string
		want     carets
	}{
		{"words", "to_upper_case", "foo bar", []caret{at(0, 1), at(0, 5)}, "FOO BAR", wantCarets(plain(0, 3), plain(0, 7))},
		{"selections", "to_upper_case", "ab cd", []caret{span(caretPos(0, 0), caretPos(0, 2)), span(caretPos(0, 3), caretPos(0, 5))}, "AB CD", wantCarets(plain(0, 2), plain(0, 5))},
		{"lower", "to_lower_case", "AB\nCD", []caret{at(0, 1), at(1, 0)}, "ab\ncd", wantCarets(plain(0, 2), plain(1, 2))},
		{"an unchanged caret stays", "to_upper_case", "FOO bar", []caret{at(0, 1), at(0, 5)}, "FOO BAR", wantCarets(plain(0, 1), plain(0, 7))},
		{"two carets in one word convert it once", "to_upper_case", "foo", []caret{at(0, 1), at(0, 2)}, "FOO", wantCarets(plain(0, 3))},
		{"a caret off any word stays", "to_upper_case", "foo  bar", []caret{at(0, 1), at(0, 4)}, "FOO  bar", wantCarets(plain(0, 3), plain(0, 4))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := multiDoc(tt.text, tt.carets...)
			run(t, e, tt.action)
			if got := text(e); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if got := caretState(e); !sameCarets(got, tt.want) {
				t.Fatalf("carets = %s, want %s", got, tt.want)
			}
			run(t, e, "undo")
			if got := text(e); got != tt.text {
				t.Fatalf("text after one undo = %q, want %q", got, tt.text)
			}
		})
	}
}

func TestTransposeCharsAtEveryCaret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		text     string
		carets   []caret
		wantText string
		want     carets
	}{
		{"two lines", "abcd\nefgh", []caret{at(0, 1), at(1, 2)}, "bacd\negfh", wantCarets(plain(0, 2), plain(1, 3))},
		{"a caret at the line start stays", "ab\ncd", []caret{at(0, 0), at(1, 1)}, "ab\ndc", wantCarets(plain(0, 0), plain(1, 2))},
		{"overlapping pairs swap once", "abcd", []caret{at(0, 1), at(0, 2)}, "bacd", wantCarets(plain(0, 2))},
		{"a caret at the line end stays", "ab\ncd", []caret{at(0, 2), at(1, 1)}, "ab\ndc", wantCarets(plain(0, 2), plain(1, 2))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := multiDoc(tt.text, tt.carets...)
			run(t, e, "transpose_chars")
			if got := text(e); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if got := caretState(e); !sameCarets(got, tt.want) {
				t.Fatalf("carets = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestDeletesAtEveryCaret(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		action   Action
		text     string
		carets   []caret
		wantText string
		want     carets
	}{
		{"word backward", "delete_word_backward", "foo bar\nbaz qux", []caret{at(0, 7), at(1, 7)}, "foo \nbaz ", wantCarets(plain(0, 4), plain(1, 4))},
		{"word forward", "delete_word_forward", "foo bar\nbaz qux", []caret{at(0, 0), at(1, 0)}, "bar\nqux", wantCarets(plain(0, 0), plain(1, 0))},
		{"word backward over a selection", "delete_word_backward", "foo bar", []caret{span(caretPos(0, 0), caretPos(0, 3)), at(0, 7)}, " ", wantCarets(plain(0, 0), plain(0, 1))},
		{"to line start", "delete_to_line_start", "abc\ndef", []caret{at(0, 2), at(1, 1)}, "c\nef", wantCarets(plain(0, 0), plain(1, 0))},
		{"to line end", "delete_to_line_end", "abc\ndef", []caret{at(0, 1), at(1, 1)}, "a\nd", wantCarets(plain(0, 1), plain(1, 1))},
		{"to line end at a line end joins lines", "delete_to_line_end", "ab\ncd\nef", []caret{at(0, 2), at(2, 0)}, "abcd\n", wantCarets(plain(0, 2), plain(1, 0))},
		{"to line start at column 0 joins lines", "delete_to_line_start", "ab\ncd\nef", []caret{at(1, 0), at(2, 1)}, "abcd\nf", wantCarets(plain(0, 2), plain(1, 0))},
		{"two words on a line", "delete_word_backward", "foo bar", []caret{at(0, 3), at(0, 7)}, " ", wantCarets(plain(0, 0), plain(0, 1))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := multiDoc(tt.text, tt.carets...)
			run(t, e, tt.action)
			if got := text(e); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if got := caretState(e); !sameCarets(got, tt.want) {
				t.Fatalf("carets = %s, want %s", got, tt.want)
			}
			run(t, e, "undo")
			if got := text(e); got != tt.text {
				t.Fatalf("text after one undo = %q, want %q", got, tt.text)
			}
		})
	}
}

func TestSortLinesAtEverySelection(t *testing.T) {
	t.Parallel()
	e := multiDoc("d\nc\nb\na\nz\ny\nx",
		span(caretPos(0, 0), caretPos(1, 1)),
		span(caretPos(4, 0), caretPos(6, 1)))
	run(t, e, "sort_lines")
	if got := text(e); got != "c\nd\nb\na\nx\ny\nz" {
		t.Fatalf("text = %q", got)
	}
	want := wantCarets(
		Caret{Cursor: caretPos(1, 1), Sel: caretSel(caretPos(0, 0), caretPos(1, 1))},
		Caret{Cursor: caretPos(6, 1), Sel: caretSel(caretPos(4, 0), caretPos(6, 1))},
	)
	if got := caretState(e); !sameCarets(got, want) {
		t.Fatalf("carets = %s, want %s", got, want)
	}
	run(t, e, "undo")
	if got := text(e); got != "d\nc\nb\na\nz\ny\nx" {
		t.Fatalf("text after one undo = %q", got)
	}
}
