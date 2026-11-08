package tui

import "github.com/tbxark/gvim/internal/tui/split"

// splitStep is the size of one resize step, in percent of the area the first
// child takes.
const splitStep = 5

// oneSplitNotice is the notice of a split action that needs a second pane.
const oneSplitNotice = "Only one split is open"

// resizeFocused changes the size of the focused pane by delta percent of its split, which
// is the inner node that holds the pane. The split's ratio is the first child's share, so
// the change is reversed when the focused pane is the second child. Either way the focused
// pane grows by delta for a positive delta. The ratio stops at the bounds.
func (m *Model) resizeFocused(delta int) {
	parent, first, ok := split.Parent(m.splits.Root, m.splits.Focus)
	if !ok {
		m.notice = oneSplitNotice
		return
	}
	if !first {
		delta = -delta
	}
	parent.Ratio = split.ClampRatio(parent.Share() + delta)
	m.syncViewport()
	m.revealCaretVisual(0)
}

// toggleMaximize shows only the focused pane, or, when it is already shown alone, lays out
// every pane again. The state is one flag, so the pane shown follows the focus. A tree with
// one pane has nothing to maximize, and a compare view keeps both of its sides.
func (m *Model) toggleMaximize() {
	switch {
	case m.splits.Root.Leaf():
		m.notice = oneSplitNotice
		return
	case m.splits.comparing:
		m.notice = "Maximize does not apply to a compare view"
		return
	}
	m.splits.maximized = !m.splits.maximized
	m.syncViewport()
	m.revealCaretVisual(0)
}
