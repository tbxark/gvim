package split

import "github.com/tbxark/gvim/session"

// TabState is a pane's viewport state as the capture sees it. The focused leaf's
// fields in the tree can be stale (the editor's active tab lives in the editor),
// so Capture takes the focused pane's tab and scroll position explicitly, exactly
// as splitInput does for painting. Capture reads it without touching the editor.
type TabState struct {
	Tab    int // the editor tab index the pane shows
	Top    int // the first visible buffer line
	TopRow int // the scrolled rows of that line; compare views only
	// ModeOf returns a leaf's Markdown view override ("source"/"rendered", empty:
	// document default). It is optional; nil leaves every ViewMode empty.
	ModeOf func(*Node) string
}

// Capture returns the saved layout of t, or nil when the layout cannot be restored
// faithfully (a single pane, a compare view state the tree cannot name, a leaf
// whose tab the session does not save, or a missing focused leaf). fileOf maps an
// editor tab index to its index in the session's file list and to that file's path;
// it reports false for a tab that the session will not save (a virtual buffer, an
// unnamed unmodified buffer, or a tab whose document is not readable). A single
// pane is nil because a session without a layout already restores one pane, so old
// files stay untouched.
//
// Capture is pure: it reads the tree only, never the editor and never the disk.
func Capture(t Tree, focus TabState, fileOf func(tab int) (file int, path string, ok bool)) *session.SplitLayout {
	if t.Root == nil || t.Root.Leaf() || t.Focus == nil {
		return nil
	}
	leaves := t.Root.Leaves()
	if len(leaves) > session.MaxSplitLeaves || treeDepth(t.Root) > session.MaxSplitDepth {
		return nil
	}
	focusIdx := -1
	for i, leaf := range leaves {
		if leaf == t.Focus {
			focusIdx = i
			break
		}
	}
	if focusIdx < 0 {
		return nil
	}
	n, ok := captureNode(t.Root, t.Focus, focus, fileOf)
	if !ok {
		return nil
	}
	return &session.SplitLayout{Root: *n, Focus: focusIdx}
}

// treeDepth is the depth of the tree, one for a leaf.
func treeDepth(n *Node) int {
	if n == nil {
		return 0
	}
	if n.Leaf() {
		return 1
	}
	return 1 + max(treeDepth(n.A), treeDepth(n.B))
}

// captureNode saves one node of the tree.
func captureNode(n *Node, focus *Node, focusState TabState, fileOf func(int) (int, string, bool)) (*session.SplitNode, bool) {
	if n == nil {
		return nil, false
	}
	if n.Leaf() {
		tab, top, topRow := n.Tab, n.Top, n.TopRow
		if n == focus {
			tab, top, topRow = focusState.Tab, focusState.Top, focusState.TopRow
		}
		file, path, ok := fileOf(tab)
		if !ok || path == "" || file < 0 {
			return nil, false
		}
		mode := ""
		if focusState.ModeOf != nil {
			mode = focusState.ModeOf(n)
		}
		if mode == "" {
			mode = n.ViewMode
		}
		return &session.SplitNode{File: file, Path: path, Top: top, TopRow: topRow, ViewMode: mode}, true
	}
	a, ok := captureNode(n.A, focus, focusState, fileOf)
	if !ok {
		return nil, false
	}
	b, ok := captureNode(n.B, focus, focusState, fileOf)
	if !ok {
		return nil, false
	}
	return &session.SplitNode{Dir: splitDirName(n.Dir), Ratio: n.Ratio, A: a, B: b}, true
}

// splitDirName is the session name of a direction.
func splitDirName(d Dir) string {
	if d == Stacked {
		return session.SplitStacked
	}
	return session.SplitSideBySide
}

// Apply rebuilds t from a saved layout. It validates l for nFiles first (shape,
// ratios, depth and leaf bounds, focus); then resolves every leaf through tabOf,
// which maps a saved Session.Files index to the editor tab restored for it. If the
// layout is invalid, or any leaf cannot be resolved to exactly one restored tab,
// Apply changes nothing and reports false: the caller keeps its one-pane tree
// rather than showing a wrong file in a pane. tabOf must implement the duplicate
// rule (same path saved twice maps to the matching restored tab occurrence); Apply
// itself only trusts its answers.
//
// Apply is the mutating half of persistence and runs on the restore path only.
func Apply(t *Tree, l *session.SplitLayout, nFiles int, tabOf func(file int) (tab int, ok bool)) bool {
	if t == nil || !l.Valid(nFiles) {
		return false
	}
	leaves := l.Root.Leaves()
	tabs := make([]int, len(leaves))
	for i, file := range leaves {
		tab, ok := tabOf(file)
		if !ok || tab < 0 {
			return false
		}
		tabs[i] = tab
	}
	next := 0
	root, ok := applyNode(&l.Root, tabs, &next)
	if !ok {
		return false
	}
	nodes := root.Leaves()
	if l.Focus < 0 || l.Focus >= len(nodes) {
		return false
	}
	t.Root = root
	t.Focus = nodes[l.Focus]
	return true
}

// applyNode rebuilds one node; tabs are the resolved editor tabs in reading order.
func applyNode(n *session.SplitNode, tabs []int, next *int) (*Node, bool) {
	if n == nil || *next >= len(tabs) {
		return nil, false
	}
	if n.Leaf() {
		tab := tabs[*next]
		*next++
		return &Node{Tab: tab, Top: n.Top, TopRow: n.TopRow, ViewMode: n.ViewMode}, true
	}
	a, ok := applyNode(n.A, tabs, next)
	if !ok {
		return nil, false
	}
	b, ok := applyNode(n.B, tabs, next)
	if !ok {
		return nil, false
	}
	return &Node{Dir: splitDirOf(n.Dir), Ratio: n.Ratio, A: a, B: b}, true
}

// splitDirOf maps a session direction name back to a Dir.
func splitDirOf(name string) Dir {
	if name == session.SplitStacked {
		return Stacked
	}
	return SideBySide
}
