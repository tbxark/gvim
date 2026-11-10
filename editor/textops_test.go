package editor

import (
	"errors"
	"slices"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// placed sets up the caret or, when sel is set, the selection of a test case.
type placed struct {
	caret buffer.Pos
	Sel   *buffer.Selection
}

func (p placed) apply(e *Editor) {
	if p.Sel != nil {
		e.SelectRange(p.Sel.Anchor, p.Sel.Head)
		return
	}
	e.SetCursor(p.caret, false)
}

func TestDuplicateLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name       string
		Text       string
		At         placed
		wantText   string
		wantCursor buffer.Pos
		wantSel    *buffer.Selection
	}{
		{
			Name:       "middle line keeps the caret column",
			Text:       "a\nb\nc",
			At:         placed{caret: buffer.Pos{Line: 1, Col: 1}},
			wantText:   "a\nb\nb\nc",
			wantCursor: buffer.Pos{Line: 2, Col: 1},
		},
		{
			Name:       "first line",
			Text:       "a\nb",
			At:         placed{caret: buffer.Pos{Line: 0, Col: 0}},
			wantText:   "a\na\nb",
			wantCursor: buffer.Pos{Line: 1, Col: 0},
		},
		{
			Name:       "last line without a newline",
			Text:       "a\nb",
			At:         placed{caret: buffer.Pos{Line: 1, Col: 0}},
			wantText:   "a\nb\nb",
			wantCursor: buffer.Pos{Line: 2, Col: 0},
		},
		{
			Name:       "single line document",
			Text:       "hello world",
			At:         placed{caret: buffer.Pos{Line: 0, Col: 3}},
			wantText:   "hello world\nhello world",
			wantCursor: buffer.Pos{Line: 1, Col: 3},
		},
		{
			Name: "selection over two lines is duplicated and kept",
			Text: "a\nb\nc",
			At: placed{Sel: &buffer.Selection{
				Anchor: buffer.Pos{Line: 0, Col: 0},
				Head:   buffer.Pos{Line: 2, Col: 0},
			}},
			wantText:   "a\nb\na\nb\nc",
			wantCursor: buffer.Pos{Line: 4, Col: 0},
			wantSel: &buffer.Selection{
				Anchor: buffer.Pos{Line: 2, Col: 0},
				Head:   buffer.Pos{Line: 4, Col: 0},
			},
		},
		{
			Name: "selection ending at column 0 leaves that line out",
			Text: "a\nb\nc",
			At: placed{Sel: &buffer.Selection{
				Anchor: buffer.Pos{Line: 0, Col: 1},
				Head:   buffer.Pos{Line: 1, Col: 0},
			}},
			wantText:   "a\na\nb\nc",
			wantCursor: buffer.Pos{Line: 2, Col: 0},
			wantSel: &buffer.Selection{
				Anchor: buffer.Pos{Line: 1, Col: 1},
				Head:   buffer.Pos{Line: 2, Col: 0},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, tt.Text)
			tt.At.apply(e)
			run(t, e, "duplicate_line")
			d := e.Active()
			if got := text(e); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if d.Cursor != tt.wantCursor {
				t.Fatalf("caret = %+v, want %+v", d.Cursor, tt.wantCursor)
			}
			if !selectionEqual(d.Sel, tt.wantSel) {
				t.Fatalf("selection = %+v, want %+v", d.Sel, tt.wantSel)
			}
		})
	}
}

func TestDuplicateLineIsOneUndoStep(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "a\nb")
	run(t, e, "duplicate_line", "undo")
	if got := text(e); got != "a\nb" {
		t.Fatalf("after undo = %q, want the original text", got)
	}
}

func TestDeleteLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name       string
		Text       string
		At         placed
		wantText   string
		wantCursor buffer.Pos
	}{
		{
			Name:       "middle line",
			Text:       "a\nb\nc",
			At:         placed{caret: buffer.Pos{Line: 1, Col: 1}},
			wantText:   "a\nc",
			wantCursor: buffer.Pos{Line: 1, Col: 0},
		},
		{
			Name:       "first line",
			Text:       "a\nb",
			At:         placed{caret: buffer.Pos{Line: 0, Col: 1}},
			wantText:   "b",
			wantCursor: buffer.Pos{Line: 0, Col: 0},
		},
		{
			Name:       "last line without a newline leaves an empty line",
			Text:       "a\nb",
			At:         placed{caret: buffer.Pos{Line: 1, Col: 0}},
			wantText:   "a\n",
			wantCursor: buffer.Pos{Line: 1, Col: 0},
		},
		{
			Name:       "only line",
			Text:       "abc",
			At:         placed{caret: buffer.Pos{Line: 0, Col: 2}},
			wantText:   "",
			wantCursor: buffer.Pos{Line: 0, Col: 0},
		},
		{
			Name: "selection is cleared and only the caret line goes",
			Text: "a\nb\nc",
			At: placed{Sel: &buffer.Selection{
				Anchor: buffer.Pos{Line: 0, Col: 0},
				Head:   buffer.Pos{Line: 1, Col: 1},
			}},
			wantText:   "a\nc",
			wantCursor: buffer.Pos{Line: 1, Col: 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, tt.Text)
			tt.At.apply(e)
			run(t, e, "delete_line")
			d := e.Active()
			if got := text(e); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if d.Cursor != tt.wantCursor {
				t.Fatalf("caret = %+v, want %+v", d.Cursor, tt.wantCursor)
			}
			if d.Sel != nil {
				t.Fatalf("selection = %+v, want none", d.Sel)
			}
		})
	}
}

func TestOpenLineLeavesCaretInPlace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name     string
		Text     string
		caret    buffer.Pos
		wantText string
	}{
		{"inside the line", "abcd", buffer.Pos{Line: 0, Col: 2}, "ab\ncd"},
		{"at the end", "ab", buffer.Pos{Line: 0, Col: 2}, "ab\n"},
		{"at the start", "ab", buffer.Pos{Line: 0, Col: 0}, "\nab"},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, tt.Text)
			e.SetCursor(tt.caret, false)
			run(t, e, "open_line")
			if got := text(e); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if got := e.Active().Cursor; got != tt.caret {
				t.Fatalf("caret = %+v, want it to stay at %+v", got, tt.caret)
			}
		})
	}
}

func TestOpenLineClearsSelectionWithoutDeletingIt(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "abc")
	e.SelectRange(buffer.Pos{Line: 0, Col: 0}, buffer.Pos{Line: 0, Col: 2})
	run(t, e, "open_line")
	d := e.Active()
	if got := text(e); got != "ab\nc" {
		t.Fatalf("text = %q, want the selected text kept", got)
	}
	if d.Sel != nil || d.Cursor != (buffer.Pos{Line: 0, Col: 2}) {
		t.Fatalf("selection = %+v caret = %+v, want none and column 2", d.Sel, d.Cursor)
	}
}

func TestSelectLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name       string
		Text       string
		caret      buffer.Pos
		wantSel    buffer.Selection
		wantCursor buffer.Pos
	}{
		{
			Name:       "middle line takes its newline",
			Text:       "a\nb\nc",
			caret:      buffer.Pos{Line: 1, Col: 0},
			wantSel:    buffer.Selection{Anchor: buffer.Pos{Line: 1, Col: 0}, Head: buffer.Pos{Line: 2, Col: 0}},
			wantCursor: buffer.Pos{Line: 2, Col: 0},
		},
		{
			Name:       "last line ends at the end of the document",
			Text:       "a\nb",
			caret:      buffer.Pos{Line: 1, Col: 0},
			wantSel:    buffer.Selection{Anchor: buffer.Pos{Line: 1, Col: 0}, Head: buffer.Pos{Line: 1, Col: 1}},
			wantCursor: buffer.Pos{Line: 1, Col: 1},
		},
		{
			Name:       "caret mid-line selects the whole line",
			Text:       "ab\ncd",
			caret:      buffer.Pos{Line: 0, Col: 1},
			wantSel:    buffer.Selection{Anchor: buffer.Pos{Line: 0, Col: 0}, Head: buffer.Pos{Line: 1, Col: 0}},
			wantCursor: buffer.Pos{Line: 1, Col: 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, tt.Text)
			e.SetCursor(tt.caret, false)
			run(t, e, "select_line")
			d := e.Active()
			if got := text(e); got != tt.Text {
				t.Fatalf("text = %q, want it unchanged", got)
			}
			if !selectionEqual(d.Sel, &tt.wantSel) || d.Cursor != tt.wantCursor {
				t.Fatalf("selection = %+v caret = %+v, want %+v caret %+v", d.Sel, d.Cursor, tt.wantSel, tt.wantCursor)
			}
		})
	}
}

func TestExpandLineSelection(t *testing.T) {
	t.Parallel()
	const doc = "a\nb\nc"
	tests := []struct {
		Name    string
		At      placed
		presses int
		wantSel *buffer.Selection
	}{
		{
			Name:    "no selection takes the caret line",
			At:      placed{caret: buffer.Pos{Line: 1, Col: 0}},
			presses: 1,
			wantSel: &buffer.Selection{Anchor: buffer.Pos{Line: 1, Col: 0}, Head: buffer.Pos{Line: 2, Col: 0}},
		},
		{
			Name:    "a second press grows by one line",
			At:      placed{caret: buffer.Pos{Line: 1, Col: 0}},
			presses: 2,
			wantSel: &buffer.Selection{Anchor: buffer.Pos{Line: 1, Col: 0}, Head: buffer.Pos{Line: 2, Col: 1}},
		},
		{
			Name: "upward selection squares off over the lines it covers",
			At: placed{Sel: &buffer.Selection{
				Anchor: buffer.Pos{Line: 2, Col: 0},
				Head:   buffer.Pos{Line: 1, Col: 0},
			}},
			presses: 1,
			wantSel: &buffer.Selection{Anchor: buffer.Pos{Line: 1, Col: 0}, Head: buffer.Pos{Line: 2, Col: 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, doc)
			tt.At.apply(e)
			for range tt.presses {
				run(t, e, "expand_line_selection")
			}
			d := e.Active()
			if got := text(e); got != doc {
				t.Fatalf("text = %q, want it unchanged", got)
			}
			if !selectionEqual(d.Sel, tt.wantSel) {
				t.Fatalf("selection = %+v, want %+v", d.Sel, tt.wantSel)
			}
			if tt.wantSel != nil && d.Cursor != tt.wantSel.Head {
				t.Fatalf("caret = %+v, want the selection head %+v", d.Cursor, tt.wantSel.Head)
			}
		})
	}
}

// TestExpandLineSelectionAtEveryCaret: Fresh's expand_line_selection grows every
// caret's line selection, not just the primary one; repeated presses add one line per
// caret while the selections stay separate.
func TestExpandLineSelectionAtEveryCaret(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "a\nb\nc\nd")
	run(t, e, "add_cursor_below") // primary line 1, extra caret line 0
	run(t, e, "expand_line_selection")
	d := e.Active()
	if d.Sel == nil || d.Sel.Anchor != (buffer.Pos{Line: 1}) || d.Sel.Head != (buffer.Pos{Line: 2}) {
		t.Fatalf("primary selection = %+v, want 1:0 to 2:0", d.Sel)
	}
	extras := d.Cursors()
	if len(extras) != 1 || extras[0].Sel == nil || extras[0].Sel.Anchor != (buffer.Pos{Line: 0}) || extras[0].Sel.Head != (buffer.Pos{Line: 1}) {
		t.Fatalf("extra caret = %+v, want line 0 selection", extras)
	}

	// A repeat grows the same caret's line selection; the single-caret test above covers
	// the continuous one-line-per-press walk without the adjacent selections meeting.
	run(t, e, "expand_line_selection")
	if d := e.Active(); d.Sel == nil {
		t.Fatal("a repeated multi-caret expand left no primary selection")
	}
}

func TestTransposeChars(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name       string
		Text       string
		caret      buffer.Pos
		wantText   string
		wantCursor buffer.Pos
	}{
		{"swaps around the caret and moves past the pair", "abc", buffer.Pos{Line: 0, Col: 2}, "acb", buffer.Pos{Line: 0, Col: 3}},
		{"second and third characters", "abcd", buffer.Pos{Line: 0, Col: 1}, "bacd", buffer.Pos{Line: 0, Col: 2}},
		{"start of a line does nothing", "abc", buffer.Pos{Line: 0, Col: 0}, "abc", buffer.Pos{Line: 0, Col: 0}},
		{"end of the document does nothing", "ab", buffer.Pos{Line: 0, Col: 2}, "ab", buffer.Pos{Line: 0, Col: 2}},
		{"end of a line does nothing", "ab\ncd", buffer.Pos{Line: 0, Col: 2}, "ab\ncd", buffer.Pos{Line: 0, Col: 2}},
		{"start of a later line does nothing", "ab\ncd", buffer.Pos{Line: 1, Col: 0}, "ab\ncd", buffer.Pos{Line: 1, Col: 0}},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, tt.Text)
			e.SetCursor(tt.caret, false)
			run(t, e, "transpose_chars")
			if got := text(e); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if got := e.Active().Cursor; got != tt.wantCursor {
				t.Fatalf("caret = %+v, want %+v", got, tt.wantCursor)
			}
		})
	}
}

func TestTransposeCharsIgnoresSelection(t *testing.T) {
	t.Parallel()
	e := docWith(Options{}, "abc")
	e.SelectRange(buffer.Pos{Line: 0, Col: 0}, buffer.Pos{Line: 0, Col: 2})
	run(t, e, "transpose_chars")
	d := e.Active()
	if got := text(e); got != "acb" {
		t.Fatalf("text = %q, want the characters around the caret swapped", got)
	}
	if d.Sel != nil {
		t.Fatalf("selection = %+v, want it cleared by the edit", d.Sel)
	}
}

func TestCaseConversion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name       string
		action     Action
		Text       string
		At         placed
		wantText   string
		wantCursor buffer.Pos
	}{
		{
			Name:       "upper word under the caret",
			action:     "to_upper_case",
			Text:       "hello world",
			At:         placed{caret: buffer.Pos{Line: 0, Col: 2}},
			wantText:   "HELLO world",
			wantCursor: buffer.Pos{Line: 0, Col: 5},
		},
		{
			Name:       "upper word when the caret is just after it",
			action:     "to_upper_case",
			Text:       "hello world",
			At:         placed{caret: buffer.Pos{Line: 0, Col: 5}},
			wantText:   "HELLO world",
			wantCursor: buffer.Pos{Line: 0, Col: 5},
		},
		{
			Name:   "upper selection",
			action: "to_upper_case",
			Text:   "hello world",
			At: placed{Sel: &buffer.Selection{
				Anchor: buffer.Pos{Line: 0, Col: 6},
				Head:   buffer.Pos{Line: 0, Col: 11},
			}},
			wantText:   "hello WORLD",
			wantCursor: buffer.Pos{Line: 0, Col: 11},
		},
		{
			Name:       "lower word at the start of a line",
			action:     "to_lower_case",
			Text:       "HELLO World",
			At:         placed{caret: buffer.Pos{Line: 0, Col: 0}},
			wantText:   "hello World",
			wantCursor: buffer.Pos{Line: 0, Col: 5},
		},
		{
			Name:       "underscore and digits belong to the word",
			action:     "to_upper_case",
			Text:       "a_1 b",
			At:         placed{caret: buffer.Pos{Line: 0, Col: 1}},
			wantText:   "A_1 b",
			wantCursor: buffer.Pos{Line: 0, Col: 3},
		},
		{
			Name:       "no word at the caret does nothing",
			action:     "to_upper_case",
			Text:       "a  b",
			At:         placed{caret: buffer.Pos{Line: 0, Col: 2}},
			wantText:   "a  b",
			wantCursor: buffer.Pos{Line: 0, Col: 2},
		},
		{
			Name:       "text already in the target case does nothing",
			action:     "to_upper_case",
			Text:       "HELLO",
			At:         placed{caret: buffer.Pos{Line: 0, Col: 0}},
			wantText:   "HELLO",
			wantCursor: buffer.Pos{Line: 0, Col: 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docWith(Options{}, tt.Text)
			tt.At.apply(e)
			run(t, e, tt.action)
			d := e.Active()
			if got := text(e); got != tt.wantText {
				t.Fatalf("text = %q, want %q", got, tt.wantText)
			}
			if d.Cursor != tt.wantCursor {
				t.Fatalf("caret = %+v, want %+v", d.Cursor, tt.wantCursor)
			}
			// A conversion of a selection keeps it selected; a word has none.
			if tt.At.Sel != nil && tt.wantText != tt.Text && !selectionEqual(d.Sel, tt.At.Sel) {
				t.Fatalf("selection = %+v, want %+v kept", d.Sel, tt.At.Sel)
			}
			if tt.At.Sel == nil && d.Sel != nil {
				t.Fatalf("selection = %+v, want none", d.Sel)
			}
		})
	}
}

// TestTrackActionsAreHandled checks that every action this track added is
// registered. Each one may still fail for a reason of its own; what must not
// happen is an unknown-action error.
func TestTrackActionsAreHandled(t *testing.T) {
	t.Parallel()
	names := []Action{
		"dedent_selection", "delete_line", "duplicate_line", "expand_line_selection",
		"move_to_paragraph_up", "move_to_paragraph_down", "select_to_paragraph_up",
		"select_to_paragraph_down", "to_upper_case", "to_lower_case", "transpose_chars",
		"toggle_comment", "open_line", "select_line", "scroll_up", "scroll_down", "recenter",
	}
	for _, name := range names {
		if !slices.Contains(Actions(), name) {
			t.Errorf("%s is not registered", name)
			continue
		}
		e := docWith(Options{}, "a\nb")
		if r := e.Do(name, nil); errors.Is(r.Err, ErrUnknownAction) {
			t.Errorf("%s returned ErrUnknownAction", name)
		}
	}
}

// selectionEqual reports whether two selections are the same, nil included.
func selectionEqual(got, want *buffer.Selection) bool {
	if got == nil || want == nil {
		return got == want
	}
	return *got == *want
}
