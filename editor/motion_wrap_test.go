package editor

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// wrapEvery returns a RowMapper that breaks a line every width runes.
func wrapEvery(e *Editor, width int) RowMapper {
	return func(line int) []int {
		n := e.Active().Buffer.LineLen(line)
		var starts []int
		for s := 0; s < max(n, 1); s += width {
			starts = append(starts, s)
		}
		return starts
	}
}

// wrapText has a line of 25 runes (rows at 0, 10, 20), a short line, a line of 12
// runes (rows at 0, 10), and a plain line.
var wrapText = strings.Join([]string{strings.Repeat("a", 25), "xyz", strings.Repeat("b", 12), "end"}, "\n")

func TestWrappedVerticalMotionMovesByDisplayRow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name  string
		Start buffer.Pos
		steps []Action
		Want  buffer.Pos
	}{
		{"down moves to the next row of the line", caretPos(0, 3), []Action{"move_down"}, caretPos(0, 13)},
		{"down twice reaches the last row", caretPos(0, 3), []Action{"move_down", "move_down"}, caretPos(0, 23)},
		{"down from the last row goes to the next line", caretPos(0, 23), []Action{"move_down"}, caretPos(1, 3)},
		{"up from a short line lands on the last row above", caretPos(1, 2), []Action{"move_up"}, caretPos(0, 22)},
		{"up moves to the previous row", caretPos(0, 14), []Action{"move_up"}, caretPos(0, 4)},
		{"the goal column survives a short row", caretPos(0, 7), []Action{"move_down", "move_down", "move_down", "move_down"}, caretPos(2, 7)},
		{"a short last row clamps the goal column", caretPos(0, 7), []Action{"move_down", "move_down", "move_down", "move_down", "move_down"}, caretPos(2, 12)},
		{"up through a wrapped line keeps the goal column", caretPos(2, 7), []Action{"move_up", "move_up", "move_up", "move_up"}, caretPos(0, 7)},
		{"a row end is not the next row start", caretPos(0, 9), []Action{"move_down"}, caretPos(0, 19)},
		{"shift down selects to the next row", caretPos(0, 3), []Action{"select_down"}, caretPos(0, 13)},
		{"up on the first row of the first line goes to the start", caretPos(0, 3), []Action{"move_up"}, caretPos(0, 0)},
		{"down on the last line goes to the end", caretPos(3, 1), []Action{"move_down"}, caretPos(3, 3)},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docAt(wrapText, tt.Start.Line, tt.Start.Col)
			e.SetRowMapper(wrapEvery(e, 10))
			run(t, e, tt.steps...)
			if got := e.Active().Cursor; got != tt.Want {
				t.Fatalf("caret = %+v, want %+v", got, tt.Want)
			}
		})
	}
}

func TestWrappedShiftDownKeepsTheAnchor(t *testing.T) {
	t.Parallel()
	e := docAt(wrapText, 0, 3)
	e.SetRowMapper(wrapEvery(e, 10))
	run(t, e, "select_down", "select_down")
	d := e.Active()
	if d.Sel == nil || d.Sel.Anchor != caretPos(0, 3) || d.Cursor != caretPos(0, 23) {
		t.Fatalf("selection %+v caret %+v, want anchor 0:3 and caret 0:23", d.Sel, d.Cursor)
	}
}

func TestWrappedHomeAndEnd(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name  string
		Start buffer.Pos
		steps []Action
		Want  buffer.Pos
	}{
		{"home on a continuation row goes to the row start", caretPos(0, 15), []Action{"smart_home"}, caretPos(0, 10)},
		{"home again goes to the previous row start", caretPos(0, 15), []Action{"smart_home", "smart_home"}, caretPos(0, 0)},
		{"home on the first row is the smart home", caretPos(0, 4), []Action{"smart_home"}, caretPos(0, 0)},
		{"move_line_start walks the rows back", caretPos(0, 25), []Action{"move_line_start", "move_line_start", "move_line_start"}, caretPos(0, 0)},
		{"end goes to the last character of the row", caretPos(0, 3), []Action{"move_line_end"}, caretPos(0, 9)},
		{"end again goes to the next row end", caretPos(0, 3), []Action{"move_line_end", "move_line_end"}, caretPos(0, 19)},
		{"end on the last row goes to the line end", caretPos(0, 3), []Action{"move_line_end", "move_line_end", "move_line_end", "move_line_end"}, caretPos(0, 25)},
		{"end on an unwrapped line is the line end", caretPos(1, 0), []Action{"move_line_end"}, caretPos(1, 3)},
		{"shift end selects to the row end", caretPos(0, 3), []Action{"select_line_end"}, caretPos(0, 9)},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docAt(wrapText, tt.Start.Line, tt.Start.Col)
			e.SetRowMapper(wrapEvery(e, 10))
			run(t, e, tt.steps...)
			if got := e.Active().Cursor; got != tt.Want {
				t.Fatalf("caret = %+v, want %+v", got, tt.Want)
			}
		})
	}
}

func TestWithoutARowMapperMotionIsByLine(t *testing.T) {
	t.Parallel()
	e := docAt(wrapText, 0, 3)
	run(t, e, "move_down")
	if got := e.Active().Cursor; got != caretPos(1, 3) {
		t.Fatalf("caret = %+v, want 1:3", got)
	}
	run(t, e, "move_up", "move_line_end")
	if got := e.Active().Cursor; got != caretPos(0, 25) {
		t.Fatalf("caret = %+v, want the line end 0:25", got)
	}
}

// A page inside a wrapped line taller than the viewport moves by display rows. A page of
// buffer lines would jump over the line and its middle rows could never be reached.
func TestPageInATallWrappedLineMovesByRows(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("a", 100) + "\nend" // ten rows of ten, in a viewport of eight
	tests := []struct {
		Name  string
		Start buffer.Pos
		act   Action
		Want  buffer.Pos
	}{
		{"page down moves one page of rows", caretPos(0, 3), "move_page_down", caretPos(0, 53)}, // 8 rows less 3 of overlap
		{"page up moves back", caretPos(0, 83), "move_page_up", caretPos(0, 33)},
		{"page down on the last rows leaves the line", caretPos(0, 63), "move_page_down", caretPos(1, 3)},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e := docAt(text, tt.Start.Line, tt.Start.Col)
			e.SetViewport(8, 20)
			e.SetRowMapper(wrapEvery(e, 10))
			run(t, e, tt.act)
			if got := e.Active().Cursor; got != tt.Want {
				t.Fatalf("caret = %+v, want %+v", got, tt.Want)
			}
		})
	}
}
