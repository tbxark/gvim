package ui

import (
	"slices"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/diff"
)

// openCompare shows the tabs left and right side by side as a compare view, with the
// focus on the left one. It reports false when either tab does not exist.
func (m *Model) openCompare(left, right int) bool {
	n := len(m.ed.Tabs())
	if left < 0 || right < 0 || left >= n || right >= n {
		return false
	}
	a, b := &splitNode{tab: left}, &splitNode{tab: right}
	m.splits.root = &splitNode{dir: sideBySide, a: a, b: b}
	m.splits.focus = nil
	m.splits.comparing = true
	m.focusLeaf(a)
	return true
}

// compareStep moves the focused side of the compare view to the next or previous
// difference, at column 0 of the line where it starts on that side. The other side
// scrolls so that the line where the same difference starts on its side is at the top.
// The differences come from compareHunks, and the search wraps around at both ends.
func (m *Model) compareStep(forward bool) {
	if !m.splits.comparing {
		m.notice = "No compare view is open"
		return
	}
	hunks, ok := m.compareHunks()
	if !ok {
		m.notice = "Compare: the other tab has no document"
		return
	}
	if len(hunks) == 0 {
		m.notice = "No differences"
		return
	}
	nodes := [2]*splitNode{m.splits.root.a, m.splits.root.b}
	docs := [2]Document{m.leafDoc(nodes[0]), m.leafDoc(nodes[1])}
	side := 0
	if m.splits.focus == nodes[1] {
		side = 1
	}
	last := docs[side].LineCount() - 1
	h := hunks[pickHunk(hunks, side, docs[side].Cursor().Line, last, forward)]
	lineFocused := min(hunkStart(h, side), last)
	lineOther := min(hunkStart(h, 1-side), docs[1-side].LineCount()-1)
	m.ed.SetCursor(buffer.Pos{Line: lineFocused}, false)
	m.ed.ScrollTo(lineFocused)
	nodes[1-side].top = max(lineOther, 0)
}

// compareHunks returns the differences between the two panes of the compare view, as
// diff.Lines gives them. Each hunk gives where it starts on each side, and the hunk
// list does not depend on the focus. A renderer paints the lines the hunks cover. The
// diff runs on each call, so a renderer that calls it on every frame should cache the
// result. ok is false when there is no compare view, or when a side has no document.
func (m *Model) compareHunks() (hunks []diff.Hunk, ok bool) {
	if !m.splits.comparing {
		return nil, false
	}
	left, right := m.leafDoc(m.splits.root.a), m.leafDoc(m.splits.root.b)
	if left == nil || right == nil {
		return nil, false
	}
	return diff.Lines(textLines(left), textLines(right)), true
}

// compareSwitch swaps the two sides of the compare view. The focus stays with the
// document it was on.
func (m *Model) compareSwitch() {
	if !m.splits.comparing {
		m.notice = "No compare view is open"
		return
	}
	r := m.splits.root
	r.a, r.b = r.b, r.a
}

// hunkStart is the line where hunk h starts on side: 0 for the left document and 1
// for the right one.
func hunkStart(h diff.Hunk, side int) int {
	if side == 0 {
		return h.OldStart
	}
	return h.NewStart
}

// pickHunk returns the index of the hunk after line on side when forward is set, and
// before it otherwise. A hunk's start is clamped to last, the last line index of the
// focused document, so a difference at the end of the text is compared where the
// cursor can stand. The search wraps past either end. It returns -1 when hunks is
// empty.
func pickHunk(hunks []diff.Hunk, side, line, last int, forward bool) int {
	if len(hunks) == 0 {
		return -1
	}
	start := func(i int) int { return min(hunkStart(hunks[i], side), last) }
	if forward {
		for i := range hunks {
			if start(i) > line {
				return i
			}
		}
		return 0
	}
	for i := range slices.Backward(hunks) {
		if start(i) < line {
			return i
		}
	}
	return len(hunks) - 1
}

// textLines returns the lines of doc, for diff.Lines.
func textLines(doc Document) []string {
	lines := make([]string, doc.LineCount())
	for i := range lines {
		lines[i] = doc.Line(i)
	}
	return lines
}
