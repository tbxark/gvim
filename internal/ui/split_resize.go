package ui

// The size of a resize step and the bounds of a split's ratio, in percent of the area
// the first child takes. The bounds keep both children in view.
const (
	splitStep     = 5
	splitMinRatio = 10
	splitMaxRatio = 90
)

// oneSplitNotice is the notice of a split action that needs a second pane.
const oneSplitNotice = "Only one split is open"

// clampRatio keeps a ratio within splitMinRatio and splitMaxRatio.
func clampRatio(r int) int {
	return min(max(r, splitMinRatio), splitMaxRatio)
}

// ratioAt is the ratio that gives the first child first of avail cells, rounded to the
// nearest percent and kept within the bounds. avail must be positive.
func ratioAt(first, avail int) int {
	return clampRatio((first*100 + avail/2) / avail)
}

// parentOf returns the inner node that has n as a child, and whether n is its first
// child. It returns false when n is the root or is not in t.
func parentOf(t, n *splitNode) (parent *splitNode, first, ok bool) {
	if t.leaf() {
		return nil, false, false
	}
	switch n {
	case t.a:
		return t, true, true
	case t.b:
		return t, false, true
	}
	if p, f, found := parentOf(t.a, n); found {
		return p, f, true
	}
	return parentOf(t.b, n)
}

// resizeFocused changes the size of the focused pane by delta percent of its split, which
// is the inner node that holds the pane. The split's ratio is the first child's share, so
// the change is reversed when the focused pane is the second child. Either way the focused
// pane grows by delta for a positive delta. The ratio stops at the bounds.
func (m *Model) resizeFocused(delta int) {
	parent, first, ok := parentOf(m.splits.root, m.splits.focus)
	if !ok {
		m.notice = oneSplitNotice
		return
	}
	if !first {
		delta = -delta
	}
	parent.ratio = clampRatio(parent.share() + delta)
	m.syncViewport()
	m.revealCaretVisual()
}

// toggleMaximize shows only the focused pane, or, when it is already shown alone, lays out
// every pane again. The state is one flag, so the pane shown follows the focus. A tree with
// one pane has nothing to maximize, and a compare view keeps both of its sides.
func (m *Model) toggleMaximize() {
	switch {
	case m.splits.root.leaf():
		m.notice = oneSplitNotice
		return
	case m.splits.comparing:
		m.notice = "Maximize does not apply to a compare view"
		return
	}
	m.splits.maximized = !m.splits.maximized
	m.syncViewport()
	m.revealCaretVisual()
}
