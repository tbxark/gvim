package restore

import "github.com/tbxark/gvim/session"

// SplitLayoutFor returns layout when it is structurally valid over files and every
// leaf still names the very file at its index, so a saved pane can never be
// resolved to a different file after a failed open or a changed session. A nil or
// stale layout returns nil: the session keeps its single-pane meaning rather than
// restoring a wrong pane. A single-pane layout is accepted by the format but the
// capture never writes one (a session without a layout already restores one pane).
func SplitLayoutFor(files []session.FileState, layout *session.SplitLayout) *session.SplitLayout {
	if layout == nil || !layout.Valid(len(files)) {
		return nil
	}
	for _, idx := range layout.Root.Leaves() {
		if files[idx].Path == "" || files[idx].Path != layoutLeafPath(&layout.Root, idx) {
			return nil
		}
	}
	return layout
}

// layoutLeafPath is the saved path of the leaf that names file idx. The layout is
// validated first, so a leaf exists; the walk returns the first match.
func layoutLeafPath(n *session.SplitNode, idx int) string {
	if n == nil {
		return ""
	}
	if n.Leaf() {
		if n.File == idx {
			return n.Path
		}
		return ""
	}
	if p := layoutLeafPath(n.A, idx); p != "" {
		return p
	}
	return layoutLeafPath(n.B, idx)
}
