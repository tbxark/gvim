// Package split holds the split tree of the editor panes and the geometry that
// places them: the tree nodes, the pane and border rectangles, and the pure
// functions that divide a band between leaves. It is a leaf package: it imports
// only the shared space kit.
//
// The root model keeps the controller methods and the live state; it reads and
// writes the exported tree through this package.
package split

// Dir is how an inner node of the split tree divides its area.
type Dir int

const (
	// SideBySide puts the children left and right (split_vertical).
	SideBySide Dir = iota
	// Stacked puts the children top and bottom (split_horizontal).
	Stacked
)

// Even is the share of the first child in an even split, and the share a zero
// Ratio stands for.
const Even = 50

// Node is a node of the split tree. A leaf (A and B nil) is one editor pane. It
// shows the tab Tab, and Top is its viewport top while another pane has the
// focus. The focused leaf takes both from the editor, so its own fields are
// stale until the focus leaves it. An inner node divides its area between A,
// the top or left child, and B, the bottom or right child; Ratio is the
// percentage A takes.
//
// A leaf also has ID, the window id of the plugin API. It is given on first use
// (Tree.IDOf) and stays with the window through splits, closes and focus
// changes; a split passes it to the child that keeps the old pane.
type Node struct {
	Dir   Dir
	A, B  *Node
	Ratio int // the percentage of the area the first child takes; zero means Even
	Tab   int
	Top   int
	// TopRow is how many display rows of line Top are scrolled off, for a pane of
	// a compare view: it lets the pane start inside the filler rows after a line.
	TopRow int
	ID     uint64 // the window id, or zero until Tree.IDOf gives one
	// Label is the plugin split label (tier 1c-C), or "" for none.
	Label string
	// ViewMode is the pane's Markdown view override ("source"/"rendered"), or ""
	// for the document's own page view. It is persisted in a dual split's leaves.
	ViewMode string
}

// Share returns the percentage of the area the first child takes.
func (n *Node) Share() int {
	if n.Ratio == 0 {
		return Even
	}
	return n.Ratio
}

// Leaf reports whether n has no children.
func (n *Node) Leaf() bool { return n.A == nil }

// Leaves returns the leaves of the tree in reading order: the first child
// before the second, depth first.
func (n *Node) Leaves() []*Node {
	if n.Leaf() {
		return []*Node{n}
	}
	return append(n.A.Leaves(), n.B.Leaves()...)
}

// ParentOf returns the inner node of the tree n whose child is leaf, or nil.
func (n *Node) ParentOf(leaf *Node) *Node {
	if n.Leaf() {
		return nil
	}
	if n.A == leaf || n.B == leaf {
		return n
	}
	if p := n.A.ParentOf(leaf); p != nil {
		return p
	}
	return n.B.ParentOf(leaf)
}

// Tree is the tree state of the split: the tree itself and the focused leaf,
// which is always a leaf of Root, plus the last window id given out.
type Tree struct {
	Root   *Node // the tree; one leaf when nothing is split
	Focus  *Node // the focused leaf, always a leaf of Root
	LastID uint64
}

// NewTree returns the state with one leaf, which has the focus.
func NewTree() Tree {
	leaf := &Node{}
	return Tree{Root: leaf, Focus: leaf}
}

// IDOf returns the window id of leaf n, giving it the next one when it has none.
func (t *Tree) IDOf(n *Node) uint64 {
	if n.ID == 0 {
		t.LastID++
		n.ID = t.LastID
	}
	return n.ID
}

// RemoveLeaf returns the tree t without the leaf n. The parent of n is replaced
// by n's sibling, which keeps its own state. t must have more than one leaf.
func RemoveLeaf(t, n *Node) *Node {
	if t.Leaf() {
		return t
	}
	switch n {
	case t.A:
		return t.B
	case t.B:
		return t.A
	}
	t.A = RemoveLeaf(t.A, n)
	t.B = RemoveLeaf(t.B, n)
	return t
}

// Parent returns the inner node of t that has n as a child, and whether n is its
// first child. It returns false when n is the root or is not in t.
func Parent(t, n *Node) (parent *Node, first, ok bool) {
	if t.Leaf() {
		return nil, false, false
	}
	switch n {
	case t.A:
		return t, true, true
	case t.B:
		return t, false, true
	}
	if p, f, found := Parent(t.A, n); found {
		return p, f, true
	}
	return Parent(t.B, n)
}
