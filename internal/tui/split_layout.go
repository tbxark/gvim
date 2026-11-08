package tui

import "github.com/tbxark/gvim/internal/tui/space"

// rect is a rectangle of frame cells, the shared geometry type of space.Rect.
type rect = space.Rect

// splitPane is one editor pane of the body, placed by computeLayout. Its rectangle
// holds the gutter, the text and, on the last column, the scrollbar. The pane's
// painter and hit test work in cells relative to rect.x and rect.y.
type splitPane struct {
	rect
	tab       int  // the tab the pane shows
	top       int  // its first visible line
	focused   bool // the pane has the keyboard, the caret and the status row
	gutter    int  // columns of line numbers before the text
	textX     int  // the first text column
	textW     int  // the text columns
	scrollbar bool // the last column is the scrollbar
}

// splitBorder is the one-cell line between two panes: a column when the panes sit
// side by side, and a row when they are stacked.
type splitBorder struct {
	rect
	vertical bool
	node     *splitNode // the inner node whose two children the line separates
	area     rect       // the rectangle that node divides, which the line lies in
}

// leafInfo is the state of one leaf that the layout needs. The layout takes them in
// reading order, the order of splitNode.leaves.
type leafInfo struct {
	tab     int  // the tab shown
	top     int  // the viewport top
	lines   int  // the line count of the shown document, which sizes the gutter
	focused bool // the leaf has the focus
}

// splitSpan divides total cells, less the one-cell line between two children, between
// the children. The first child takes ratio percent of the cells left, rounded to the
// nearest, so an even ratio gives the first child the larger half of an odd remainder.
// When there are two or more cells, each child keeps at least one.
func splitSpan(total, ratio int) (first, second int) {
	avail := max(total-1, 0)
	first = (avail*ratio + 50) / 100
	if avail >= 2 {
		first = min(max(first, 1), avail-1)
	}
	return first, avail - first
}

// maximizedRects gives the focused leaf the whole band and every other leaf an empty
// rectangle. Each leaf keeps its index, so the panes still match the leaves in reading
// order.
func maximizedRects(leaves []leafInfo, band rect) []rect {
	rects := make([]rect, len(leaves))
	for i, li := range leaves {
		if li.focused {
			rects[i] = band
		}
	}
	return rects
}

// borderOf returns the line that divides node in the layout. It returns false when the
// layout has no line for node, as when the node has no area or is not in the tree.
func (l layout) borderOf(node *splitNode) (splitBorder, bool) {
	for _, b := range l.borders {
		if b.node == node {
			return b, true
		}
	}
	return splitBorder{}, false
}

// placeSplits divides r between the leaves of n. The rectangle of each leaf is
// appended to rects in reading order, and the line between the children of each
// inner node is appended to borders. A line in a zero-size area is not added.
func placeSplits(n *splitNode, r rect, rects *[]rect, borders *[]splitBorder) {
	if n.leaf() {
		*rects = append(*rects, r)
		return
	}
	var a, b rect
	var line splitBorder
	if n.dir == sideBySide {
		first, second := splitSpan(r.W, n.share())
		a = rect{X: r.X, Y: r.Y, W: first, H: r.H}
		b = rect{X: r.X + first + 1, Y: r.Y, W: second, H: r.H}
		line = splitBorder{rect: rect{X: r.X + first, Y: r.Y, W: 1, H: r.H}, vertical: true, node: n, area: r}
		if r.W > 0 {
			*borders = append(*borders, line)
		}
	} else {
		first, second := splitSpan(r.H, n.share())
		a = rect{X: r.X, Y: r.Y, W: r.W, H: first}
		b = rect{X: r.X, Y: r.Y + first + 1, W: r.W, H: second}
		line = splitBorder{rect: rect{X: r.X, Y: r.Y + first, W: r.W, H: 1}, vertical: false, node: n, area: r}
		if r.H > 0 {
			*borders = append(*borders, line)
		}
	}
	placeSplits(n.a, a, rects, borders)
	placeSplits(n.b, b, rects, borders)
}

// focusedPane returns the pane that has the focus. Without panes it returns the
// editor band, so the callers that read the pane size still get the band's size.
func (l layout) focusedPane() splitPane {
	if l.focus >= 0 && l.focus < len(l.panes) {
		return l.panes[l.focus]
	}
	return splitPane{X: l.editorX, Y: l.bodyTop, W: max(l.w-l.editorX, 0), H: max(l.bodyBottom-l.bodyTop+1, 0)}
}

// placePanes sets the panes, the lines between them and the focus from the split
// tree in in. With no tree the band is one pane that shows in.lines. The gutter,
// textX, textW and scrollbar fields of layout then describe the focused pane.
func (l *layout) placePanes(in layoutInput) {
	shape, leaves := in.split, in.leaves
	if shape == nil {
		shape = &splitNode{}
		leaves = []leafInfo{{tab: -1, lines: in.lines, focused: true}}
	}
	band := rect{X: l.editorX, Y: l.bodyTop, W: max(l.w-l.editorX, 0), H: max(l.bodyBottom-l.bodyTop+1, 0)}
	var rects []rect
	l.borders = nil
	if in.maximized {
		rects = maximizedRects(leaves, band)
	} else {
		placeSplits(shape, band, &rects, &l.borders)
	}
	l.panes = make([]splitPane, len(rects))
	l.focus = 0
	for i, r := range rects {
		var li leafInfo
		if i < len(leaves) {
			li = leaves[i]
		}
		// With the gutter hidden the text would start on the pane's first column, right
		// against the explorer border or the split line. The pane gives up PadX cells
		// at its left edge for that, so its text starts PadX cells in.
		if in.hideLineNumbers && r.W > space.PadX {
			r.X, r.W = r.X+space.PadX, r.W-space.PadX
		}
		p := splitPane{rect: r, tab: li.tab, top: li.top, focused: li.focused}
		if !in.hideLineNumbers {
			p.gutter = gutterWidth(li.lines)
		}
		p.textX = r.X + p.gutter
		p.textW = max(0, r.X+r.W-p.textX)
		p.scrollbar = r.H > 0 && li.lines > r.H
		l.panes[i] = p
		if li.focused {
			l.focus = i
		}
	}
	f := l.focusedPane()
	l.gutter, l.textX, l.textW, l.scrollbar = f.gutter, f.textX, f.textW, f.scrollbar
}
