package editor

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
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
		name  string
		start buffer.Pos
		steps []Action
		want  buffer.Pos
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
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docAt(wrapText, tt.start.Line, tt.start.Col)
			e.SetRowMapper(wrapEvery(e, 10))
			run(t, e, tt.steps...)
			if got := e.Active().Cursor; got != tt.want {
				t.Fatalf("caret = %+v, want %+v", got, tt.want)
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
		name  string
		start buffer.Pos
		steps []Action
		want  buffer.Pos
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
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := docAt(wrapText, tt.start.Line, tt.start.Col)
			e.SetRowMapper(wrapEvery(e, 10))
			run(t, e, tt.steps...)
			if got := e.Active().Cursor; got != tt.want {
				t.Fatalf("caret = %+v, want %+v", got, tt.want)
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
