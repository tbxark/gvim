package session

import "fmt"

// SplitLayout is the saved pane layout of a session: a tree of editor panes with
// the focused pane, and each pane's tab (as an index into Session.Files) and
// scroll position. It is additive and optional: a session file without it restores
// as one pane, exactly as before, and this package keeps the format version at 1.
//
// The tree mirrors the editor's in-memory split tree (internal/tui/split.Node):
// an inner node has Dir and two children (A first, B second) and a ratio; a leaf
// has no children and names its tab. A layout is presentation state: a layout that
// does not validate (bad ratios, too many panes, a file index outside Session.Files,
// a malformed tree) is ignored on restore instead of failing the session, and the
// editor comes back as one pane.
type SplitLayout struct {
	// Root is the tree; it must be a leaf or a complete split tree.
	Root SplitNode `json:"root"`
	// Focus is the focused leaf's index in reading order (the leaves of Root,
	// first child before second, depth first). Zero is the first pane.
	Focus int `json:"focus,omitempty"`
	// Dual marks the Markdown dual view: Root must be a side-by-side two-leaf
	// split of the same saved file, the first leaf is the source and the second
	// the composed/rendered pane, and Focus is zero (the source, Fresh's policy).
	// It is optional and additive: an older session or a normal split omits it.
	Dual bool `json:"dual,omitempty"`
}

// SplitNode is one node of SplitLayout: a leaf when A and B are nil, else an inner
// node. A leaf names its tab by index into Session.Files and repeats that file's
// path; Path is the identity a restore resolves against the editor's restored tabs,
// so a layout never silently shows the wrong file. Top and TopRow are the pane's
// saved scroll position.
type SplitNode struct {
	// Dir is the inner node's direction: SplitSideBySide or SplitStacked. It is
	// empty on a leaf.
	Dir string `json:"dir,omitempty"`
	// Ratio is the percentage of the area the first child takes (1..99); zero
	// means an even split. Unused on a leaf.
	Ratio int `json:"ratio,omitempty"`
	// A and B are the two children of an inner node; both are nil on a leaf.
	A *SplitNode `json:"a,omitempty"`
	B *SplitNode `json:"b,omitempty"`
	// File is a leaf's index into Session.Files.
	File int `json:"file,omitempty"`
	// Path is the leaf file's path, as Session.Files[File].Path. It is the stable
	// identity a restore uses to find the restored tab; an empty path (an unnamed
	// buffer) cannot be restored as a pane and makes the whole layout fall back.
	Path string `json:"path,omitempty"`
	// Top is the leaf's viewport top line and TopRow its scrolled rows into that
	// line (compare views only). Zero is the first line.
	Top    int `json:"top,omitempty"`
	TopRow int `json:"topRow,omitempty"`
	// ViewMode is the leaf's Markdown view override: "source" or "rendered"; empty
	// means the document's own page view. Persisted for the dual view's two panes,
	// so the saved split keeps each side's mode; an older session omits it.
	ViewMode string `json:"viewMode,omitempty"`
}

// Split directions, the data names of split.Dir.
const (
	SplitSideBySide = "side_by_side"
	SplitStacked    = "stacked"
)

// Bounds of a saved layout: an editor can always fall back to one pane, so a
// hand-edited or corrupt tree may not make the loader recurse or allocate without
// end.
const (
	MaxSplitLeaves = 16
	MaxSplitDepth  = 16
)

// Leaf reports whether n is a leaf.
func (n *SplitNode) Leaf() bool { return n.A == nil && n.B == nil }

// Valid reports whether l is a structurally valid layout over nFiles session files.
// It checks the node shape, the direction names, ratios (0 or 1..99), the leaf
// bound and depth, every leaf's File index and non-empty Path, and that Focus names
// an existing leaf. It does not read the files themselves: a path that is no longer
// open is found by the restore's path lookup.
func (l *SplitLayout) Valid(nFiles int) bool {
	if l == nil {
		return false
	}
	leaves, depth, ok := l.Root.valid(nFiles, 1)
	if !ok || leaves < 1 || leaves > MaxSplitLeaves || depth > MaxSplitDepth {
		return false
	}
	if l.Focus < 0 || l.Focus >= leaves {
		return false
	}
	if l.Dual && !l.validDual() {
		return false
	}
	return true
}

// validDual validates the optional Markdown dual marker: two side-by-side leaves of
// the same saved file, source first and rendered second, focus on the source. Empty
// ViewMode values are accepted for backward-compatible writers and inferred by the
// restore; a non-empty value must name the leaf's side.
func (l *SplitLayout) validDual() bool {
	if l.Focus != 0 {
		return false
	}
	root := l.Root
	if root.Dir != SplitSideBySide || root.A == nil || root.B == nil || !root.A.Leaf() || !root.B.Leaf() {
		return false
	}
	if root.A.File != root.B.File || root.A.Path == "" || root.A.Path != root.B.Path {
		return false
	}
	if root.A.ViewMode != "" && root.A.ViewMode != "source" {
		return false
	}
	if root.B.ViewMode != "" && root.B.ViewMode != "rendered" {
		return false
	}
	return true
}

// valid checks n and its subtree. It returns the leaf count and the subtree depth.
func (n *SplitNode) valid(nFiles, depth int) (leaves, maxDepth int, ok bool) {
	if n == nil || depth > MaxSplitDepth {
		return 0, 0, false
	}
	switch {
	case n.Leaf():
		if n.File < 0 || n.File >= nFiles {
			return 0, 0, false
		}
		if n.Path == "" {
			return 0, 0, false
		}
		return 1, depth, true
	case n.A == nil || n.B == nil:
		return 0, 0, false
	case n.Dir != SplitSideBySide && n.Dir != SplitStacked:
		return 0, 0, false
	case n.Ratio < 0 || n.Ratio > 99:
		return 0, 0, false
	case n.Ratio > 0 && n.Ratio < 1:
		return 0, 0, false
	}
	la, da, ok := n.A.valid(nFiles, depth+1)
	if !ok {
		return 0, 0, false
	}
	lb, db, ok := n.B.valid(nFiles, depth+1)
	if !ok {
		return 0, 0, false
	}
	if la+lb > MaxSplitLeaves {
		return 0, 0, false
	}
	return la + lb, max(da, db), true
}

// Leaves returns the leaf file indexes in reading order.
func (n *SplitNode) Leaves() []int {
	if n == nil {
		return nil
	}
	if n.Leaf() {
		return []int{n.File}
	}
	return append(n.A.Leaves(), n.B.Leaves()...)
}

// ValidateSplitLayout is Valid as a free function, for callers that hold a nil
// layout from an old session.
func ValidateSplitLayout(l *SplitLayout, nFiles int) bool { return l.Valid(nFiles) }

var _ = fmt.Sprintf
