package tui

import (
	"github.com/tbxark/gvim/diff"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/internal/tui/grid"
)

// compareSide is what the compare view paints one of its sides from: the look of each
// line a hunk covers, the line of the other side that each changed line is paired with,
// and the filler rows that keep the two sides level.
type compareSide struct {
	looks   map[int]compareLook
	partner map[int]int // a paired (modified) line to its partner on the other side
	fill    *editorpane.Fillers
	marks   []editorpane.TrackMark // one per hunk, on the scrollbar track
}

// newCompareSides builds the two sides of the view from the hunks.
func newCompareSides(hunks []diff.Hunk) [2]compareSide {
	var out [2]compareSide
	for side := range out {
		out[side] = compareSide{
			looks:   compareLines(hunks, side),
			partner: comparePartners(hunks, side),
			fill:    alignFillers(hunks, side),
		}
		out[side].marks = trackMarks(hunks, side, out[side].fill)
	}
	return out
}

// trackMarks returns the scrollbar mark of each hunk on side: the rows the hunk takes, in the
// rows of the whole aligned text, which are the same on both sides, and its color as Fresh
// picks it: the added color on the left of a hunk that adds lines there only, the removed
// color on the right of one that removes them, the modified color where lines are paired, and
// otherwise the color of the side's own lines.
func trackMarks(hunks []diff.Hunk, side int, fill *editorpane.Fillers) []editorpane.TrackMark {
	out := make([]editorpane.TrackMark, 0, len(hunks))
	for _, h := range hunks {
		start, own, theirs := h.OldStart, h.OldLines, h.NewLines
		lone := compareRemovedColor
		if side == 1 {
			start, own, theirs = h.NewStart, h.NewLines, h.OldLines
			lone = compareAddedColor
		}
		var from int
		switch {
		case own > 0:
			from = fill.TextRow(start)
		case start > 0:
			from = fill.TextRow(start-1) + 1
		}
		fg := lone
		switch {
		case own == 0 && side == 0:
			fg = compareAddedColor
		case own == 0:
			fg = compareRemovedColor
		case theirs > 0:
			fg = compareModifiedColor
		}
		out = append(out, editorpane.TrackMark{
			From: from, To: from + max(own, theirs) - 1,
			Style: grid.Style{FG: fg},
		})
	}
	return out
}

// comparePartners returns, for each line of side that a change pairs with a line of the
// other side, that other line. A change pairs its first min(old, new) lines one to one.
func comparePartners(hunks []diff.Hunk, side int) map[int]int {
	out := map[int]int{}
	for _, h := range hunks {
		if h.Kind != diff.Change {
			continue
		}
		for k := range min(h.OldLines, h.NewLines) {
			if side == 0 {
				out[h.OldStart+k] = h.NewStart + k
			} else {
				out[h.NewStart+k] = h.OldStart + k
			}
		}
	}
	return out
}

// alignFillers returns the filler rows of side, 0 for the left document and 1 for the
// right one, that put the lines both sides share on the same row. The side with fewer
// lines in a change gets a filler row for each line the other side has more, right after
// the last line the change has on its side, as Fresh draws them, and above the first
// line when the change is at the very top of the text.
func alignFillers(hunks []diff.Hunk, side int) *editorpane.Fillers {
	above := 0
	after := map[int]int{}
	for _, h := range hunks {
		start, own, theirs := h.OldStart, h.OldLines, h.NewLines
		if side == 1 {
			start, own, theirs = h.NewStart, h.NewLines, h.OldLines
		}
		gap := theirs - own
		if gap <= 0 {
			continue
		}
		if line := start + own - 1; line >= 0 {
			after[line] += gap
		} else {
			above += gap
		}
	}
	return editorpane.NewFillers(above, after, grid.Style{})
}

// changedSpan returns the runes of a and b that are left once their common prefix and
// suffix are taken off, as [aFrom, aTo) and [bFrom, bTo) rune columns. It reports false
// when the two are equal. This is the part of a changed line that the view picks out.
func changedSpan(a, b []rune) (aFrom, aTo, bFrom, bTo int, ok bool) {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	if pre == len(a) && pre == len(b) {
		return 0, 0, 0, 0, false
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	return pre, len(a) - suf, pre, len(b) - suf, true
}

// hunkJump is the line a jump to hunk h puts the caret on, on side. A side with no lines in
// the hunk takes the line before the gap, right above its filler rows. Otherwise it is
// the first line of the hunk. Both are clamped to last, the last line of the document.
func hunkJump(h diff.Hunk, side, last int) int {
	start, n := h.OldStart, h.OldLines
	if side == 1 {
		start, n = h.NewStart, h.NewLines
	}
	if n == 0 && start > 0 {
		start--
	}
	return min(max(start, 0), max(last, 0))
}

// hunkAt returns the index of the hunk that the caret line is in on side, or -1. A hunk
// with no lines on that side is at its seam: the line before the gap and the line after it.
func hunkAt(hunks []diff.Hunk, side, line int) int {
	for i, h := range hunks {
		start, n := h.OldStart, h.OldLines
		if side == 1 {
			start, n = h.NewStart, h.NewLines
		}
		if n > 0 && line >= start && line < start+n || n == 0 && (line == start || line == start-1) {
			return i
		}
	}
	return -1
}
