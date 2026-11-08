package split

import "github.com/tbxark/gvim/internal/tui/space"

// The bounds of a split's ratio, in percent of the area the first child takes.
// The bounds keep both children in view.
const (
	MinRatio = 10
	MaxRatio = 90
)

// LeafInfo is the state of one leaf that the layout needs. The layout takes them
// in reading order, the order of Node.Leaves.
type LeafInfo struct {
	Tab     int  // the tab shown
	Top     int  // the viewport top
	Lines   int  // the line count of the shown document, which sizes the gutter
	Focused bool // the leaf has the focus
}

// Pane is one editor pane of the body, placed by the layout. Its rectangle holds
// the gutter, the text and, on the last column, the scrollbar. The pane's
// painter and hit test work in cells relative to the rectangle.
type Pane struct {
	space.Rect
	Tab       int  // the tab the pane shows
	Top       int  // its first visible line
	Focused   bool // the pane has the keyboard, the caret and the status row
	Gutter    int  // columns of line numbers before the text
	TextX     int  // the first text column
	TextW     int  // the text columns
	Scrollbar bool // the last column is the scrollbar
}

// Border is the one-cell line between two panes: a column when the panes sit
// side by side, and a row when they are stacked.
type Border struct {
	space.Rect
	Vertical bool       // the line is a column
	Node     *Node      // the inner node whose two children the line separates
	Area     space.Rect // the rectangle that node divides, which the line lies in
}

// Span divides total cells, less the one-cell line between two children, between
// the children. The first child takes ratio percent of the cells left, rounded to
// the nearest, so an even ratio gives the first child the larger half of an odd
// remainder. When there are two or more cells, each child keeps at least one.
func Span(total, ratio int) (first, second int) {
	avail := max(total-1, 0)
	first = (avail*ratio + 50) / 100
	if avail >= 2 {
		first = min(max(first, 1), avail-1)
	}
	return first, avail - first
}

// MaximizedRects gives the focused leaf the whole band and every other leaf an
// empty rectangle. Each leaf keeps its index, so the panes still match the
// leaves in reading order.
func MaximizedRects(leaves []LeafInfo, band space.Rect) []space.Rect {
	rects := make([]space.Rect, len(leaves))
	for i, li := range leaves {
		if li.Focused {
			rects[i] = band
		}
	}
	return rects
}

// Place divides r between the leaves of n. The rectangle of each leaf is appended
// to rects in reading order, and the line between the children of each inner node
// is appended to borders. A line in a zero-size area is not added.
func Place(n *Node, r space.Rect, rects *[]space.Rect, borders *[]Border) {
	if n.Leaf() {
		*rects = append(*rects, r)
		return
	}
	var a, b space.Rect
	var line Border
	if n.Dir == SideBySide {
		first, second := Span(r.W, n.Share())
		a = space.Rect{X: r.X, Y: r.Y, W: first, H: r.H}
		b = space.Rect{X: r.X + first + 1, Y: r.Y, W: second, H: r.H}
		line = Border{Rect: space.Rect{X: r.X + first, Y: r.Y, W: 1, H: r.H}, Vertical: true, Node: n, Area: r}
		if r.W > 0 {
			*borders = append(*borders, line)
		}
	} else {
		first, second := Span(r.H, n.Share())
		a = space.Rect{X: r.X, Y: r.Y, W: r.W, H: first}
		b = space.Rect{X: r.X, Y: r.Y + first + 1, W: r.W, H: second}
		line = Border{Rect: space.Rect{X: r.X, Y: r.Y + first, W: r.W, H: 1}, Vertical: false, Node: n, Area: r}
		if r.H > 0 {
			*borders = append(*borders, line)
		}
	}
	Place(n.A, a, rects, borders)
	Place(n.B, b, rects, borders)
}

// ClampRatio keeps a ratio within MinRatio and MaxRatio.
func ClampRatio(r int) int {
	return min(max(r, MinRatio), MaxRatio)
}

// RatioAt is the ratio that gives the first child first of avail cells, rounded
// to the nearest percent and kept within the bounds. avail must be positive.
func RatioAt(first, avail int) int {
	return ClampRatio((first*100 + avail/2) / avail)
}
