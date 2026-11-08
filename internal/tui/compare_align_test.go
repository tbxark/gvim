package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/diff"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/pluginapi"
)

// paneRowOf returns the frame row where pane i of the layout shows text in its body, or -1.
func paneRowOf(m *Model, i int, text string) int {
	l := m.layout()
	p := l.panes[i]
	g := m.paint()
	for y := p.Y; y < p.Y+p.H; y++ {
		var b strings.Builder
		for x := p.X; x < p.X+p.W; x++ {
			b.WriteString(g.At(x, y).Text)
		}
		if strings.Contains(b.String(), text) {
			return y
		}
	}
	return -1
}

// numbered returns n lines "line <i>", with the lines of skip left out and the strings of
// extra added after the line with that index.
func numbered(n int, skip map[int]bool, extra map[int][]string) []string {
	var out []string
	for i := range n {
		if !skip[i] {
			out = append(out, fmt.Sprintf("line %d", i))
		}
		out = append(out, extra[i]...)
	}
	return out
}

func TestAlignFillersFromHunks(t *testing.T) {
	tests := []struct {
		name  string
		hunks []diff.Hunk
		side  int
		above int
		after map[int]int
	}{
		{"a delete gives the right side fillers after the line before the gap",
			[]diff.Hunk{{Kind: diff.Delete, OldStart: 2, OldLines: 3, NewStart: 2}}, 1, 0, map[int]int{1: 3}},
		{"a delete leaves the left side alone",
			[]diff.Hunk{{Kind: diff.Delete, OldStart: 2, OldLines: 3, NewStart: 2}}, 0, 0, map[int]int{}},
		{"an insert gives the left side fillers",
			[]diff.Hunk{{Kind: diff.Insert, OldStart: 4, NewStart: 4, NewLines: 2}}, 0, 0, map[int]int{3: 2}},
		{"a change pairs the common lines and fills the rest after the last own line",
			[]diff.Hunk{{Kind: diff.Change, OldStart: 1, OldLines: 1, NewStart: 1, NewLines: 4}}, 0, 0, map[int]int{1: 3}},
		{"a change at the top with a shorter side fills below its own lines",
			[]diff.Hunk{{Kind: diff.Change, OldStart: 0, OldLines: 1, NewStart: 0, NewLines: 3}}, 0, 0, map[int]int{0: 2}},
		{"an insert at the very top goes above the first line",
			[]diff.Hunk{{Kind: diff.Insert, OldStart: 0, NewStart: 0, NewLines: 2}}, 0, 2, map[int]int{}},
		{"two hunks add up",
			[]diff.Hunk{
				{Kind: diff.Insert, OldStart: 2, NewStart: 2, NewLines: 1},
				{Kind: diff.Insert, OldStart: 2, NewStart: 5, NewLines: 0},
			}, 0, 0, map[int]int{1: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fl := alignFillers(tt.hunks, tt.side)
			if fl.Above != tt.above {
				t.Errorf("Above = %d, want %d", fl.Above, tt.above)
			}
			for line, n := range tt.after {
				if fl.After[line] != n {
					t.Errorf("After[%d] = %d, want %d", line, fl.After[line], n)
				}
			}
			if len(fl.After) != len(tt.after) {
				t.Errorf("After = %v, want %v", fl.After, tt.after)
			}
		})
	}
}

// TestAlignedSidesHaveTheSameRows checks, over a few texts, that the two sides of a compare
// have the same number of rows and that every line both sides share is on the same row.
func TestAlignedSidesHaveTheSameRows(t *testing.T) {
	tests := []struct {
		name        string
		left, right []string
	}{
		{"identical", []string{"a", "b"}, []string{"a", "b"}},
		{"insert in the middle", []string{"a", "b", "c"}, []string{"a", "x", "y", "b", "c"}},
		{"delete in the middle", []string{"a", "x", "y", "b", "c"}, []string{"a", "b", "c"}},
		{"change with different sizes", []string{"a", "1", "b"}, []string{"a", "2", "3", "4", "b"}},
		{"insert at the top", []string{"a"}, []string{"x", "y", "a"}},
		{"append at the end", []string{"a"}, []string{"a", "x", "y"}},
		{"everything differs", []string{"a", "b"}, []string{"x", "y", "z"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hunks := diff.Lines(tt.left, tt.right)
			l, r := alignFillers(hunks, 0), alignFillers(hunks, 1)
			if lt, rt := l.Total(len(tt.left)), r.Total(len(tt.right)); lt != rt {
				t.Fatalf("rows: left %d, right %d", lt, rt)
			}
			for i, a := range tt.left {
				j := slices.Index(tt.right, a)
				if j < 0 || !sharedLine(hunks, i, j) {
					continue
				}
				if lr, rr := l.TextRow(i), r.TextRow(j); lr != rr {
					t.Errorf("shared line %q: left row %d, right row %d", a, lr, rr)
				}
			}
		})
	}
}

// sharedLine reports whether line i of the left and line j of the right are the same line
// of the diff: both lie outside every hunk and are as far from the hunks' ends.
func sharedLine(hunks []diff.Hunk, i, j int) bool {
	shift := 0 // right line = left line + shift outside the hunks
	for _, h := range hunks {
		if i < h.OldStart {
			break
		}
		if i < h.OldStart+h.OldLines {
			return false
		}
		shift = h.NewStart + h.NewLines - (h.OldStart + h.OldLines)
	}
	return j == i+shift
}

func TestCompareViewPutsSharedLinesOnTheSameRow(t *testing.T) {
	left := numbered(30, map[int]bool{12: true, 13: true}, nil)
	right := numbered(30, nil, map[int][]string{4: {"new a", "new b", "new c"}})
	m := newCompareModel(t, left, right)
	// "line 3" is above both changes, and "line 5" is below the first one: the right side has
	// three more lines above it, which the left side holds open with filler rows.
	for _, text := range []string{"line 3 ", "line 5 ", "line 11"} {
		l, r := paneRowOf(m, 0, text), paneRowOf(m, 1, text)
		if l < 0 || l != r {
			t.Errorf("%q is on row %d on the left and row %d on the right", text, l, r)
		}
	}
}

// TestCompareScrollKeepsTheSidesLevel scrolls the compare view with the wheel over each pane
// and checks that the two panes stay on the same display rows, filler rows included.
func TestCompareScrollKeepsTheSidesLevel(t *testing.T) {
	left := numbered(80, map[int]bool{12: true, 13: true, 14: true, 15: true}, nil)
	right := numbered(80, nil, map[int][]string{4: {"new a", "new b", "new c"}, 40: {"more"}})
	for _, overPane := range []int{0, 1} {
		t.Run(fmt.Sprintf("wheel over pane %d", overPane), func(t *testing.T) {
			m := newCompareModel(t, left, right)
			l := m.layout()
			p := l.panes[overPane]
			for n := range 40 {
				m.Update(tea.MouseWheelMsg(tea.Mouse{X: p.X + p.gutter + 2, Y: l.bodyTop + 1, Button: tea.MouseWheelDown}))
				if a, b := alignedTops(t, m); a != b {
					t.Fatalf("after %d notches the panes start at rows %d and %d", n+1, a, b)
				}
			}
			if a, _ := alignedTops(t, m); a == 0 {
				t.Fatal("the view did not scroll")
			}
			// The same line is on the same frame row on both sides.
			for _, text := range []string{"line 3", "line 7", "line 30", "line 50"} {
				lr, rr := paneRowOf(m, 0, text), paneRowOf(m, 1, text)
				if lr >= 0 && rr >= 0 && lr != rr {
					t.Errorf("%s is on row %d on the left and row %d on the right", text, lr, rr)
				}
			}
			for range 80 {
				m.Update(tea.MouseWheelMsg(tea.Mouse{X: p.X + p.gutter + 2, Y: l.bodyTop + 1, Button: tea.MouseWheelUp}))
			}
			if a, b := alignedTops(t, m); a != 0 || b != 0 {
				t.Errorf("after scrolling back up the panes start at rows %d and %d, want 0", a, b)
			}
		})
	}
}

func TestCompareSwitchSideKeepsTheViewLevel(t *testing.T) {
	left := numbered(80, map[int]bool{12: true, 13: true}, nil)
	right := numbered(80, nil, map[int][]string{4: {"new a", "new b"}})
	m := newCompareModel(t, left, right)
	l := m.layout()
	for range 10 {
		m.Update(tea.MouseWheelMsg(tea.Mouse{X: l.panes[0].X + 8, Y: l.bodyTop + 1, Button: tea.MouseWheelDown}))
	}
	before, _ := alignedTops(t, m)
	m.Update(palette.RunCommandMsg{Action: "compare_switch_side"})
	a, b := alignedTops(t, m)
	if a != before || b != before {
		t.Errorf("rows %d and %d after switching the side, want both %d", a, b, before)
	}
	// The row on screen of a shared line did not move either.
	if lr, rr := paneRowOf(m, 0, "line 20"), paneRowOf(m, 1, "line 20"); lr != rr {
		t.Errorf("line 20 is on row %d and row %d", lr, rr)
	}
}

func TestComparePressLandsOnTheLineUnderThePointer(t *testing.T) {
	left := numbered(20, map[int]bool{3: true, 4: true}, nil)
	right := numbered(20, nil, nil)
	m := newCompareModel(t, left, right)
	// The right pane has lines 3 and 4 that the left lacks, so the left has two filler rows
	// after "line 2" and every left line below them is two rows lower than its number says.
	l := m.layout()
	left0, right0 := l.panes[0], l.panes[1]
	row := paneRowOf(m, 0, "line 5")
	if row != paneRowOf(m, 1, "line 5") {
		t.Fatalf("line 5 is not level: %d and %d", row, paneRowOf(m, 1, "line 5"))
	}
	_, hit, _, ok := m.paneHitIn(0, left0.textX+1, row)
	if !ok || hit.Pos.Line != 3 { // line 5 is the fourth line of the left text
		t.Errorf("a press on the left's line 5 maps to %+v, want its line 3", hit)
	}
	_, hit, _, _ = m.paneHitIn(1, right0.textX+1, row)
	if hit.Pos.Line != 5 {
		t.Errorf("a press on the right's line 5 maps to %+v, want its line 5", hit)
	}
	// A press on a filler row of the left side lands at the end of the line before it.
	filler := paneRowOf(m, 0, "line 2") + 1
	_, hit, _, _ = m.paneHitIn(0, left0.textX+1, filler)
	if hit.Pos != (buffer.Pos{Line: 2, Col: len("line 2")}) {
		t.Errorf("a press on a filler row maps to %+v, want the end of line 2", hit.Pos)
	}
	// A real press on a filler row puts the caret at the end of the line it follows, and a
	// press on its gutter does nothing to the caret.
	m.Update(tea.MouseClickMsg(tea.Mouse{X: left0.textX + 1, Y: filler, Button: tea.MouseLeft}))
	if got := m.ed.Active().Cursor(); got != (buffer.Pos{Line: 2, Col: len("line 2")}) {
		t.Errorf("caret after a press on a filler row = %+v, want the end of line 2", got)
	}
	m.ed.SetCursor(buffer.Pos{Line: 7}, false)
	m.Update(tea.MouseClickMsg(tea.Mouse{X: left0.X + 1, Y: filler, Button: tea.MouseLeft}))
	if got := m.ed.Active().Cursor(); got.Line != 7 {
		t.Errorf("a press on the gutter of a filler row moved the caret to %+v", got)
	}
}

func TestCompareFocusSwitchByPressKeepsTheView(t *testing.T) {
	left := numbered(60, map[int]bool{5: true, 6: true, 7: true}, nil)
	right := numbered(60, nil, nil)
	m := newCompareModel(t, left, right)
	l := m.layout()
	for range 3 {
		m.Update(tea.MouseWheelMsg(tea.Mouse{X: l.panes[0].X + 8, Y: l.bodyTop + 1, Button: tea.MouseWheelDown}))
	}
	before, _ := alignedTops(t, m)
	row := l.bodyTop + 2
	m.Update(tea.MouseClickMsg(tea.Mouse{X: l.panes[1].textX + 1, Y: row, Button: tea.MouseLeft}))
	if m.layout().focus != 1 {
		t.Fatal("the press did not focus the right pane")
	}
	if a, b := alignedTops(t, m); a != before || b != before {
		t.Errorf("rows %d and %d after the press, want both %d", a, b, before)
	}
}

func TestCompareDecorMarksTheLines(t *testing.T) {
	m := newCompareModel(t, compareLeft, compareRight)
	l := m.layout()
	g := m.paint()
	st, _ := m.compareState()
	// The marks "~", "-" and "+" are in the first gutter column of the rows.
	tests := []struct {
		name       string
		pane, line int
		want       string
	}{
		{"removed line on the left", 0, 1, "-"},
		{"added line on the right", 1, 3, "+"},
		{"added line at the end of the right", 1, 5, "+"},
		{"a shared line", 0, 0, " "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := l.panes[tt.pane]
			y := l.bodyTop + st.side[tt.pane].fill.TextRow(tt.line)
			if got := g.At(p.X, y).Text; got != tt.want {
				t.Errorf("mark = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestChangedSpanTrimsTheCommonEnds(t *testing.T) {
	tests := []struct {
		a, b           string
		af, at, bf, bt int
		ok             bool
	}{
		{"hello", "hello", 0, 0, 0, 0, false},
		{"hello world", "hello brave world", 6, 6, 6, 12, true},
		{"abc", "xyz", 0, 3, 0, 3, true},
		{"foo(1)", "foo(22)", 4, 5, 4, 6, true},
		{"", "abc", 0, 0, 0, 3, true},
		{"héllo", "hello", 1, 2, 1, 2, true},
		{"aaa", "aa", 2, 3, 2, 2, true},
	}
	for _, tt := range tests {
		af, at, bf, bt, ok := changedSpan([]rune(tt.a), []rune(tt.b))
		if af != tt.af || at != tt.at || bf != tt.bf || bt != tt.bt || ok != tt.ok {
			t.Errorf("changedSpan(%q, %q) = %d %d %d %d %v, want %d %d %d %d %v",
				tt.a, tt.b, af, at, bf, bt, ok, tt.af, tt.at, tt.bf, tt.bt, tt.ok)
		}
	}
}

func TestReplaceLines(t *testing.T) {
	lines := []string{"a", "bb", "ccc"}
	lineLen := func(i int) int { return len(lines[i]) }
	apply := func(e pluginapi.Edit) string {
		// Apply the edit to the text of lines, the way the editor does.
		text := strings.Join(lines, "\n")
		off := func(p pluginapi.Pos) int {
			n := 0
			for i := range p.Line {
				n += len(lines[i]) + 1
			}
			return n + p.Col
		}
		return text[:off(e.Range.From)] + e.Text + text[off(e.Range.To):]
	}
	tests := []struct {
		name       string
		start, end int
		src        []string
		want       string
	}{
		{"replace the middle line", 1, 2, []string{"X"}, "a\nX\nccc"},
		{"replace a middle line with two", 1, 2, []string{"X", "Y"}, "a\nX\nY\nccc"},
		{"replace the last line", 2, 3, []string{"X"}, "a\nbb\nX"},
		{"delete a middle line", 1, 2, nil, "a\nccc"},
		{"delete the last line", 2, 3, nil, "a\nbb"},
		{"delete the first line", 0, 1, nil, "bb\nccc"},
		{"insert in the middle", 1, 1, []string{"X"}, "a\nX\nbb\nccc"},
		{"insert at the top", 0, 0, []string{"X"}, "X\na\nbb\nccc"},
		{"append after the last line", 3, 3, []string{"X", "Y"}, "a\nbb\nccc\nX\nY"},
		{"replace everything", 0, 3, []string{"X"}, "X"},
		{"replace the tail with two lines", 1, 3, []string{"X", "Y"}, "a\nX\nY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := replaceLines(len(lines), lineLen, tt.start, tt.end, tt.src)
			if got := apply(e); got != tt.want {
				t.Errorf("result = %q, want %q (edit %+v)", got, tt.want, e)
			}
		})
	}
}

func TestHunkJumpAndHunkAt(t *testing.T) {
	// The left side of this hunk is empty: b was removed from the right, so the right
	// side has the lines [1, 3) in it and the left side has none.
	h := diff.Hunk{Kind: diff.Insert, OldStart: 1, NewStart: 1, NewLines: 2}
	if got := hunkJump(h, 0, 5); got != 0 {
		t.Errorf("jump on the empty side = %d, want 0, the line before the gap", got)
	}
	if got := hunkJump(h, 1, 5); got != 1 {
		t.Errorf("jump on the side with lines = %d, want 1", got)
	}
	if got := hunkJump(diff.Hunk{Kind: diff.Insert, OldStart: 0, NewLines: 1}, 0, 5); got != 0 {
		t.Errorf("jump to a gap at the top = %d, want 0", got)
	}
	if got := hunkJump(diff.Hunk{Kind: diff.Delete, OldStart: 9, OldLines: 1}, 0, 5); got != 5 {
		t.Errorf("jump past the end = %d, want it clamped to 5", got)
	}
	hunks := []diff.Hunk{h, {Kind: diff.Delete, OldStart: 6, OldLines: 2, NewStart: 5}}
	tests := []struct {
		side, line, want int
	}{
		{1, 0, -1}, {1, 1, 0}, {1, 2, 0}, {1, 3, -1},
		{0, 0, 0}, {0, 1, 0}, {0, 2, -1}, // the empty side: the line before the gap and the one after it
		{0, 6, 1}, {0, 7, 1}, {0, 8, -1},
		{1, 4, 1}, {1, 5, 1}, {1, 6, -1}, // the right side of the delete is empty: seam lines 4 and 5
	}
	for _, tt := range tests {
		if got := hunkAt(hunks, tt.side, tt.line); got != tt.want {
			t.Errorf("hunkAt(side %d, line %d) = %d, want %d", tt.side, tt.line, got, tt.want)
		}
	}
}

func TestCompareSwitchPutsTheCaretOnTheSameRow(t *testing.T) {
	// The right side has "new a", "new b" after line 2, so the left holds two filler rows
	// there and its line 3 is on the row of the right's line 5.
	left := numbered(20, nil, nil)
	right := numbered(20, nil, map[int][]string{2: {"new a", "new b"}})
	m := newCompareModel(t, left, right)
	m.ed.SetCursor(buffer.Pos{Line: 3, Col: 2}, false)
	m.Update(palette.RunCommandMsg{Action: "compare_switch_side"})
	if got := m.ed.Active().Cursor(); got != (buffer.Pos{Line: 5, Col: 2}) {
		t.Errorf("caret after the switch = %v, want line 5 column 2: the same row", got)
	}
	m.Update(palette.RunCommandMsg{Action: "compare_switch_side"})
	if got := m.ed.Active().Cursor(); got != (buffer.Pos{Line: 3, Col: 2}) {
		t.Errorf("caret after switching back = %v, want line 3 column 2", got)
	}
}

func TestTrackMarksCoverTheRowsOfEachHunk(t *testing.T) {
	// Left a b c d e, right a X Y d e f: b c are changed to X Y, f is added at the end.
	hunks := diff.Lines([]string{"a", "b", "c", "d", "e"}, []string{"a", "X", "Y", "d", "e", "f"})
	sides := newCompareSides(hunks)
	for side, want := range [][]editorpane.TrackMark{
		{
			{From: 1, To: 2, Style: grid.Style{FG: compareModifiedColor}},
			{From: 5, To: 5, Style: grid.Style{FG: compareAddedColor}}, // the left has a filler row after e
		},
		{
			{From: 1, To: 2, Style: grid.Style{FG: compareModifiedColor}},
			{From: 5, To: 5, Style: grid.Style{FG: compareAddedColor}},
		},
	} {
		if got := sides[side].marks; !slices.Equal(got, want) {
			t.Errorf("side %d marks = %+v, want %+v", side, got, want)
		}
	}
	// A hunk only the left has is marked in the removed color on the left and the right alike
	// would be a different story: the right side's mark is the removed color too.
	hunks = diff.Lines([]string{"a", "b", "c"}, []string{"a", "c"})
	sides = newCompareSides(hunks)
	if got := sides[0].marks; len(got) != 1 || got[0].Style.FG != compareRemovedColor || got[0].From != 1 || got[0].To != 1 {
		t.Errorf("left marks = %+v, want one removed mark on row 1", got)
	}
	if got := sides[1].marks; len(got) != 1 || got[0].Style.FG != compareRemovedColor || got[0].From != 1 || got[0].To != 1 {
		t.Errorf("right marks = %+v, want one removed mark on row 1", got)
	}
}

// TestCompareMarksLinesThatDifferOnlyInTheirEnding checks the rule of Fresh's compare: two
// lines with the same text that end differently (CRLF against LF) are marked "~" with a note
// that says how each ends, and the summary no longer calls the files identical.
func TestCompareMarksLinesThatDifferOnlyInTheirEnding(t *testing.T) {
	lines := []string{"one", "two", ""}
	f := newDocsFake(lines, lines)
	f.docs[0].eol = "CRLF"
	m := newSplitModel(t, f, 80, 24)
	if !m.openCompare(0, 1) {
		t.Fatal("openCompare(0, 1) = false")
	}
	m.compareSummary(false)
	if !strings.Contains(m.notice, "line ending") {
		t.Errorf("summary = %q, want it to say that the files differ in their line endings", m.notice)
	}
	l := m.layout()
	g := m.paint()
	rowText := func(pane, y int) string {
		p := l.panes[pane]
		var b strings.Builder
		for x := p.X; x < p.X+p.W; x++ {
			b.WriteString(g.At(x, y).Text)
		}
		return b.String()
	}
	for line := range 2 {
		y := l.bodyTop + line
		for pane, note := range []string{"CRLF line ending", "LF line ending"} {
			if got := g.At(l.panes[pane].X, y).Text; got != "~" {
				t.Errorf("pane %d line %d mark = %q, want ~", pane, line, got)
			}
			if got := rowText(pane, y); !strings.Contains(got, note) {
				t.Errorf("pane %d line %d row %q lacks the note %q", pane, line, got, note)
			}
		}
	}
	// The empty last line is no line: it has no ending to differ in.
	if got := g.At(l.panes[0].X, l.bodyTop+2).Text; got == "~" {
		t.Errorf("the empty last line is marked")
	}
	// Equal endings mark nothing.
	f.docs[1].eol = "CRLF"
	g = m.paint()
	if got := g.At(l.panes[0].X, l.bodyTop).Text; got == "~" {
		t.Errorf("a line is marked although both sides end it the same way")
	}
}

// TestCompareScrollsBothSidesSideways checks that the unfocused side of a comparison keeps the
// horizontal position of the focused one, so the two show the same columns of their lines.
func TestCompareScrollsBothSidesSideways(t *testing.T) {
	long := strings.Repeat("x", 200) + "END"
	m := newCompareModel(t, []string{long, "b"}, []string{long, "c"})
	m.ed.SetCursor(buffer.Pos{Col: 203}, false)
	m.syncViewport()
	l := m.layout()
	lefts := [2]int{}
	for i, p := range l.panes {
		doc := m.paneDoc(p)
		lefts[i], _ = m.paneGeometry(p, doc).HScroll(p.W, p.H)
	}
	if lefts[0] == 0 {
		t.Fatalf("the focused pane did not scroll sideways (left %d)", lefts[0])
	}
	if lefts[1] != lefts[0] {
		t.Errorf("first visible column: left %d, right %d, want them level", lefts[0], lefts[1])
	}
	// What the right pane paints is the same end of the line.
	g := m.paint()
	var b strings.Builder
	p := l.panes[1]
	for x := p.X; x < p.X+p.W; x++ {
		b.WriteString(g.At(x, l.bodyTop).Text)
	}
	if !strings.Contains(b.String(), "END") {
		t.Errorf("the right pane row %q does not show the end of its line", b.String())
	}
}
