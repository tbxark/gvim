package editor

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// windowEvery returns a RowWindowMapper that breaks a line every width runes, for the lines
// that have at least tallFrom runes, and lists only the rows around the column, as the pane does:
// the row before the one that holds the column and the two after it.
func windowEvery(e *Editor, width, tallFrom int) RowWindowMapper {
	return func(line, col int) ([]int, bool) {
		n := e.Active().Buffer.LineLen(line)
		if n < tallFrom {
			return nil, false
		}
		row := min(col, max(n-1, 0)) / width
		var starts []int
		for k := max(row-1, 0); k <= row+2 && k*width < max(n, 1); k++ {
			starts = append(starts, k*width)
		}
		return starts, true
	}
}

// The rows of a tall line, given as a window around the caret, move the caret as the whole list
// of rows does, for every motion that goes by row, from every column, with goal columns kept.
func TestTallRowWindowMovesLikeTheListOfRows(t *testing.T) {
	t.Parallel()
	text := strings.Join([]string{strings.Repeat("a", 95), "xyz", strings.Repeat("b", 130), "end"}, "\n")
	actions := []Action{"move_down", "move_up", "smart_home", "move_line_start", "move_line_end", "select_down", "select_up", "select_line_end"}
	rng := rand.New(rand.NewPCG(2, 9))
	for trial := range 400 {
		line := rng.IntN(4)
		col := rng.IntN(len([]rune(strings.Split(text, "\n")[line])) + 1)
		var steps []Action
		for range 1 + rng.IntN(6) {
			steps = append(steps, actions[rng.IntN(len(actions))])
		}
		list := docAt(text, line, col)
		list.SetRowMapper(wrapEvery(list, 10))
		win := docAt(text, line, col)
		win.SetRowWindowMapper(windowEvery(win, 10, 0))
		run(t, list, steps...)
		run(t, win, steps...)
		a, b := list.Active(), win.Active()
		if a.Cursor != b.Cursor || (a.Sel == nil) != (b.Sel == nil) || (a.Sel != nil && *a.Sel != *b.Sel) {
			t.Fatalf("trial %d: %v from %d:%d: the list gives caret %+v selection %+v, the window %+v %+v",
				trial, steps, line, col, a.Cursor, a.Sel, b.Cursor, b.Sel)
		}
	}
}

// Page Down and Page Up in a tall line move by rows, a page of them, and stay in the line.
func TestPageInATallLineMovesByRows(t *testing.T) {
	t.Parallel()
	text := "first\n" + strings.Repeat("w", 5000) + "\nlast"
	e := docAt(text, 1, 100)
	e.SetRowWindowMapper(windowEvery(e, 10, 100))
	e.SetViewport(5, 80)
	run(t, e, "move_page_down")
	if got := e.Active().Cursor; got.Line != 1 || got.Col <= 100 || got.Col > 100+5*10 {
		t.Fatalf("after page_down caret = %+v, want line 1 within a page of rows below column 100", got)
	}
	run(t, e, "move_page_up")
	if got := e.Active().Cursor; got.Line != 1 || got.Col < 90 || got.Col > 110 {
		t.Fatalf("after page_down and page_up caret = %+v, want about column 100 again", got)
	}
}

// End on a tall line goes to the end of the row, then the next row, and the last row ends
// at the real end of the line, however long it is.
func TestEndOnAVeryLongLineReachesTheEnd(t *testing.T) {
	t.Parallel()
	const n = 2_000_000
	e := docAt(strings.Repeat("x", n), 0, n-100)
	e.SetRowWindowMapper(windowEvery(e, 100, 100))
	run(t, e, "move_line_end", "move_line_end", "move_line_end")
	if got := e.Active().Cursor; got != caretPos(0, n) {
		t.Fatalf("caret = %+v, want the end of the line at column %d", got, n)
	}
	run(t, e, "move_document_start")
	run(t, e, "move_document_end")
	if got := e.Active().Cursor; got != caretPos(0, n) {
		t.Fatalf("move_doc_end: caret = %+v, want column %d", got, n)
	}
}
