package frame

import (
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/split"
)

// BorderOf returns the line that divides node in the Layout. It returns false when the
// Layout has no line for node, as when the node has no area or is not in the tree.
func (l Layout) BorderOf(node *split.Node) (split.Border, bool) {
	for _, b := range l.Borders {
		if b.Node == node {
			return b, true
		}
	}
	return split.Border{}, false
}

// FocusedPane returns the pane that has the focus. Without panes it returns the
// editor band, so the callers that read the pane size still get the band's size.
func (l Layout) FocusedPane() split.Pane {
	if l.Focus >= 0 && l.Focus < len(l.Panes) {
		return l.Panes[l.Focus]
	}
	return split.Pane{X: l.EditorX, Y: l.BodyTop, W: max(l.W-l.EditorX, 0), H: max(l.BodyBottom-l.BodyTop+1, 0)}
}

// placePanes sets the panes, the lines between them and the focus from the split
// tree in in. With no tree the band is one pane that shows in.lines. The gutter,
// textX, textW and scrollbar fields of Layout then describe the focused pane.
func (l *Layout) placePanes(in Input) {
	shape, leaves := in.Split, in.Leaves
	if shape == nil {
		shape = &split.Node{}
		leaves = []split.LeafInfo{{Tab: -1, Lines: in.Lines, Focused: true}}
	}
	band := space.Rect{X: l.EditorX, Y: l.BodyTop, W: max(l.W-l.EditorX, 0), H: max(l.BodyBottom-l.BodyTop+1, 0)}
	var rects []space.Rect
	l.Borders = nil
	if in.Maximized {
		rects = split.MaximizedRects(leaves, band)
	} else {
		split.Place(shape, band, &rects, &l.Borders)
	}
	l.Panes = make([]split.Pane, len(rects))
	l.Focus = 0
	for i, r := range rects {
		var li split.LeafInfo
		if i < len(leaves) {
			li = leaves[i]
		}
		// With the gutter hidden the text would start on the pane's first column, right
		// against the explorer border or the split line. The pane gives up PadX cells
		// at its left edge for that, so its text starts PadX cells in.
		if in.HideLineNumbers && r.W > space.PadX {
			r.X, r.W = r.X+space.PadX, r.W-space.PadX
		}
		p := split.Pane{Rect: r, Tab: li.Tab, Top: li.Top, Focused: li.Focused}
		if !in.HideLineNumbers {
			p.Gutter = GutterWidth(li.Lines)
		}
		p.TextX = r.X + p.Gutter
		p.TextW = max(0, r.X+r.W-p.TextX)
		p.Scrollbar = r.H > 0 && li.Lines > r.H
		l.Panes[i] = p
		if li.Focused {
			l.Focus = i
		}
	}
	f := l.FocusedPane()
	l.Gutter, l.TextX, l.TextW, l.Scrollbar = f.Gutter, f.TextX, f.TextW, f.Scrollbar
}
