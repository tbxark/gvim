package tui

// Pane-layout persistence: capture the split tree into the session
// format without touching the editor, and restore it over the editor's restored
// tabs. The split package owns the tree/session conversion and the validation; this
// file owns the tab<->file identity rules, which are the same rules restore uses to
// number the session's FileState list.

import (
	"path/filepath"

	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/split"
	"github.com/tbxark/gvim/session"
)

// SplitState returns the pane layout to save with the session, or nil when there is
// nothing to save (one pane, a compare/dual view, or a pane whose tab the session
// does not save). It reads the model only: the tree, the tabs and the focused
// document's top, never switching tabs or scrolling anything. The file indexes are
// numbered exactly as restore.WriteSession numbers its files: virtual buffers and
// unnamed, unmodified buffers are skipped; an unnamed MODIFIED buffer is saved (its
// stash restores it) but has no stable path, so a layout that shows one returns nil
// instead of writing a pane that could resolve to the wrong tab later.
func (m *Model) SplitState() *session.SplitLayout {
	if m.splits.comparing || m.splits.Root == nil || m.splits.Root.Leaf() {
		return nil
	}
	tabs := m.ed.Tabs()
	fileOfTab := make([]int, len(tabs))
	for i := range fileOfTab {
		fileOfTab[i] = -1
	}
	next := 0
	for i := range tabs {
		doc := m.docAt(i)
		if doc == nil || isVirtualDoc(doc) || (tabs[i].Path == "" && !tabs[i].Modified) {
			continue
		}
		fileOfTab[i] = next
		next++
	}
	fileOf := func(tab int) (int, string, bool) {
		if tab < 0 || tab >= len(fileOfTab) || fileOfTab[tab] < 0 {
			return 0, "", false
		}
		path := m.tabPath(tab)
		if path == "" {
			return 0, "", false
		}
		return fileOfTab[tab], path, true
	}
	focusTab := m.activeTab()
	focusTop, focusTopRow := 0, 0
	if doc := m.ed.Active(); doc != nil {
		focusTop = doc.Top()
	}
	dual := m.validDual()
	// The one-dual policy always restores with the focus on the source. A user may
	// focus the preview before saving; capture the source leaf as the focus anyway,
	// otherwise the session would record Dual=true/Focus=1 and be rejected on load.
	tree := m.splits.Tree
	if dual != nil {
		tree.Focus = dual.source
		focusTab = dual.source.Tab
		focusTop, focusTopRow = dual.source.Top, dual.source.TopRow
	}
	modeOf := func(n *split.Node) string {
		if dual != nil {
			switch n {
			case dual.source:
				return "source"
			case dual.preview:
				return "rendered"
			}
		}
		return n.ViewMode
	}
	layout := split.Capture(tree, split.TabState{
		Tab: focusTab, Top: focusTop, TopRow: focusTopRow, ModeOf: modeOf,
	}, fileOf)
	if layout != nil && dual != nil {
		layout.Dual = true
	}
	return layout
}

// RestoreSplits applies a saved pane layout over the editor's restored tabs. A nil
// layout, an invalid one, or one whose leaves do not all resolve to exactly one
// restored tab leaves the model's one-pane tree alone: a stale or ambiguous record
// must never put a different file in a pane. The saved paths are the stable
// identity; duplicates are resolved by occurrence rank in the saved file list and
// the restored tab order, and a path that no longer opened (a deleted file) makes
// the whole layout fall back. On success the saved focused pane becomes the active
// tab and scroll position. It returns whether the layout was applied.
func (m *Model) RestoreSplits(layout *session.SplitLayout, files []session.FileState) bool {
	if layout == nil || m.splits.comparing || m.splits.dual != nil || m.ed == nil {
		return false
	}
	if !layout.Valid(len(files)) || !validDualLayout(layout) {
		return false
	}
	// Every leaf must still name the file at its saved index: a hand-edited or
	// stale record cannot borrow another file's path.
	for _, idx := range layout.Root.Leaves() {
		if files[idx].Path == "" || files[idx].Path != splitLeafPath(&layout.Root, idx) {
			return false
		}
	}
	tabOfFile := splitTabMapping(files, m.ed.Tabs())
	if len(tabOfFile) == 0 {
		return false
	}
	tree := m.splits.Tree
	if !split.Apply(&tree, layout, len(files), func(file int) (int, bool) {
		if file < 0 || file >= len(tabOfFile) || tabOfFile[file] < 0 {
			return 0, false
		}
		return tabOfFile[file], true
	}) {
		return false
	}
	m.splits.Tree = tree
	m.splits.tabs = len(m.ed.Tabs())
	m.splits.seenTab = m.activeTab()
	m.splits.prevTab = -1
	// Bring the editor to the saved focused pane: focusLeaf copies the current
	// tab/top into the old focus, so clear it first to make it apply the restored
	// leaf rather than early-returning (the restored focus is already the focus).
	focus := m.splits.Focus
	m.splits.Focus = nil
	m.focusLeaf(focus)
	if layout.Dual {
		leaves := m.splits.Root.Leaves()
		if len(leaves) != 2 {
			return false
		}
		leaves[0].ViewMode = "source"
		leaves[1].ViewMode = "rendered"
		m.splits.dual = &dualView{tab: leaves[0].Tab, source: leaves[0], preview: leaves[1]}
	}
	return true
}

// validDualLayout checks the optional dual marker: two side-by-side leaves of the
// same saved file, focus on the source (first leaf), exactly the one-dual policy.
// ViewMode records are inferred when an older writer omitted them; a dual marker on
// any other shape makes the whole layout fall back to one pane.
func validDualLayout(layout *session.SplitLayout) bool {
	if layout == nil || !layout.Dual {
		return true
	}
	if layout.Focus != 0 {
		return false
	}
	root := layout.Root
	if root.Dir != session.SplitSideBySide || root.A == nil || root.B == nil || !root.A.Leaf() || !root.B.Leaf() {
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

// splitLeafPath is the path of the saved leaf that names file idx.
func splitLeafPath(n *session.SplitNode, idx int) string {
	if n == nil {
		return ""
	}
	if n.Leaf() {
		if n.File == idx {
			return n.Path
		}
		return ""
	}
	if p := splitLeafPath(n.A, idx); p != "" {
		return p
	}
	return splitLeafPath(n.B, idx)
}

// splitTabMapping maps each saved session file to the restored tab it names. The
// identity is the cleaned path plus its occurrence rank: a file saved twice maps to
// the first and second restored tab with that path, so duplicates never cross-wire.
// A saved file whose tab did not come back (a deleted file, a failed restore) gets
// -1; a restored tab with the same path but no matching rank is ignored.
func splitTabMapping(files []session.FileState, tabs []frame.TabInfo) []int {
	byPath := make(map[string][]int, len(tabs))
	for i, tab := range tabs {
		if tab.Path == "" {
			continue
		}
		key := filepath.Clean(tab.Path)
		byPath[key] = append(byPath[key], i)
	}
	out := make([]int, len(files))
	rank := make(map[string]int, len(files))
	for i, f := range files {
		out[i] = -1
		if f.Path == "" {
			continue
		}
		key := filepath.Clean(f.Path)
		r := rank[key]
		rank[key] = r + 1
		if list := byPath[key]; r < len(list) {
			out[i] = list[r]
		}
	}
	return out
}
