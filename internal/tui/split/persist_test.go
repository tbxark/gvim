package split

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/session"
)

// filesOf is a fileOf resolver over a path list; tab i is path[i].
func filesOf(paths []string) func(int) (int, string, bool) {
	return func(tab int) (int, string, bool) {
		if tab < 0 || tab >= len(paths) || paths[tab] == "" {
			return 0, "", false
		}
		return tab, paths[tab], true
	}
}

// tabResolver maps a saved file index to that same tab, for the round-trip tests.
func tabResolver(n int) func(int) (int, bool) {
	return func(file int) (int, bool) {
		if file < 0 || file >= n {
			return 0, false
		}
		return file, true
	}
}

func TestSplitCaptureApplyRoundTrip(t *testing.T) {
	t.Parallel()
	// Two panes side by side: left tab 0 at top 3, right tab 1 at top 7.
	left := &Node{Tab: 0, Top: 3}
	right := &Node{Tab: 1, Top: 7}
	root := &Node{Dir: SideBySide, Ratio: 35, A: left, B: right}
	tree := Tree{Root: root, Focus: right}
	layout := Capture(tree, TabState{Tab: 1, Top: 8}, filesOf([]string{"/w/a.txt", "/w/b.txt"}))
	if layout == nil || !layout.Valid(2) {
		t.Fatalf("captured layout = %+v", layout)
	}
	if layout.Root.Dir != session.SplitSideBySide || layout.Root.Ratio != 35 || layout.Focus != 1 {
		t.Fatalf("captured root = %+v focus %d", layout.Root, layout.Focus)
	}
	if layout.Root.A.File != 0 || layout.Root.B.File != 1 || layout.Root.B.Path != "/w/b.txt" || layout.Root.B.Top != 8 {
		t.Fatalf("captured leaves = %+v / %+v", layout.Root.A, layout.Root.B)
	}
	restored := NewTree()
	if !Apply(&restored, layout, 2, tabResolver(2)) {
		t.Fatal("Apply rejected its own capture")
	}
	if restored.Root.Dir != SideBySide || restored.Root.Ratio != 35 {
		t.Fatalf("restored root = %+v", restored.Root)
	}
	leaves := restored.Root.Leaves()
	if len(leaves) != 2 || leaves[0].Tab != 0 || leaves[0].Top != 3 || leaves[1].Tab != 1 || leaves[1].Top != 8 {
		t.Fatalf("restored leaves = %+v / %+v", leaves[0], leaves[1])
	}
	if restored.Focus != leaves[1] {
		t.Fatal("the focused pane was not restored")
	}
}

func TestSplitCaptureNestedAndFocusedState(t *testing.T) {
	t.Parallel()
	// Stacked: top pane split side-by-side into two, bottom one. Focus the bottom.
	top := &Node{Dir: SideBySide, Ratio: 0, A: &Node{Tab: 0, Top: 1}, B: &Node{Tab: 1}}
	bottom := &Node{Tab: 2, Top: 9}
	root := &Node{Dir: Stacked, Ratio: 70, A: top, B: bottom}
	tree := Tree{Root: root, Focus: bottom}
	layout := Capture(tree, TabState{Tab: 2, Top: 12}, filesOf([]string{"/a", "/b", "/c"}))
	if layout == nil || !layout.Valid(3) {
		t.Fatalf("nested capture = %+v", layout)
	}
	if strings.Join(strings.Fields(strings.Repeat("x", 0)), "") != "" {
		t.Fatal("unreachable")
	}
	if layout.Root.Dir != session.SplitStacked || layout.Root.Ratio != 70 {
		t.Fatalf("nested root = %+v", layout.Root)
	}
	if layout.Root.A.Dir != session.SplitSideBySide || layout.Root.A.Ratio != 0 || layout.Root.A.A.File != 0 || layout.Root.A.B.File != 1 {
		t.Fatalf("nested first child = %+v", layout.Root.A)
	}
	if layout.Root.B.File != 2 || layout.Root.B.Top != 12 || layout.Focus != 2 {
		t.Fatalf("focused bottom = %+v focus %d", layout.Root.B, layout.Focus)
	}
	restored := NewTree()
	if !Apply(&restored, layout, 3, tabResolver(3)) {
		t.Fatal("Apply rejected the nested capture")
	}
	if got := len(restored.Root.Leaves()); got != 3 {
		t.Fatalf("restored leaves = %d, want 3", got)
	}
	if restored.Root.Ratio != 70 || restored.Root.A.Ratio != 0 {
		t.Fatalf("ratios not restored: %+v", restored.Root)
	}
}

func TestSplitCaptureRefusesUnrepresentableLayouts(t *testing.T) {
	t.Parallel()
	single := NewTree()
	if got := Capture(single, TabState{Tab: 0}, filesOf([]string{"/a"})); got != nil {
		t.Fatalf("a single pane captured %+v, want nil", got)
	}
	root := &Node{Dir: SideBySide, A: &Node{Tab: 0}, B: &Node{Tab: 1}}
	tree := Tree{Root: root, Focus: root.A}
	// A tab the session does not save (virtual buffer) makes the whole layout nil.
	bad := func(tab int) (int, string, bool) {
		if tab == 1 {
			return 0, "", false
		}
		return tab, "/a", true
	}
	if got := Capture(tree, TabState{Tab: 0}, bad); got != nil {
		t.Fatalf("capture with an unresolvable leaf = %+v, want nil", got)
	}
	// An unnamed file has no stable path identity, so it is unrepresentable too.
	unnamed := func(tab int) (int, string, bool) { return tab, "", true }
	if got := Capture(tree, TabState{Tab: 0}, unnamed); got != nil {
		t.Fatalf("capture with an unnamed leaf = %+v, want nil", got)
	}
}

func TestSplitApplyRejectsInvalidLayouts(t *testing.T) {
	t.Parallel()
	valid := Tree{Root: &Node{Dir: SideBySide, A: &Node{Tab: 0}, B: &Node{Tab: 1}}, Focus: nil}
	valid.Focus = valid.Root.A
	before := *valid.Root
	tests := []struct {
		name   string
		layout *session.SplitLayout
		files  int
		tabOf  func(int) (int, bool)
	}{
		{name: "nil", layout: nil, files: 2, tabOf: tabResolver(2)},
		{name: "bad direction", files: 2, tabOf: tabResolver(2), layout: &session.SplitLayout{Root: session.SplitNode{Dir: "diagonal", A: &session.SplitNode{File: 0, Path: "/a"}, B: &session.SplitNode{File: 1, Path: "/b"}}}},
		{name: "bad ratio", files: 2, tabOf: tabResolver(2), layout: &session.SplitLayout{Root: session.SplitNode{Dir: session.SplitSideBySide, Ratio: 120, A: &session.SplitNode{File: 0, Path: "/a"}, B: &session.SplitNode{File: 1, Path: "/b"}}}},
		{name: "file out of range", files: 2, tabOf: tabResolver(2), layout: &session.SplitLayout{Root: session.SplitNode{Dir: session.SplitSideBySide, A: &session.SplitNode{File: 0, Path: "/a"}, B: &session.SplitNode{File: 2, Path: "/c"}}}},
		{name: "empty path", files: 2, tabOf: tabResolver(2), layout: &session.SplitLayout{Root: session.SplitNode{Dir: session.SplitSideBySide, A: &session.SplitNode{File: 0}, B: &session.SplitNode{File: 1, Path: "/b"}}}},
		{name: "one child", files: 2, tabOf: tabResolver(2), layout: &session.SplitLayout{Root: session.SplitNode{Dir: session.SplitSideBySide, A: &session.SplitNode{File: 0, Path: "/a"}}}},
		{name: "focus out of range", files: 2, tabOf: tabResolver(2), layout: &session.SplitLayout{Focus: 2, Root: session.SplitNode{Dir: session.SplitSideBySide, A: &session.SplitNode{File: 0, Path: "/a"}, B: &session.SplitNode{File: 1, Path: "/b"}}}},
		{name: "unresolved leaf", files: 2, tabOf: func(int) (int, bool) { return 0, false }, layout: &session.SplitLayout{Root: session.SplitNode{Dir: session.SplitSideBySide, A: &session.SplitNode{File: 0, Path: "/a"}, B: &session.SplitNode{File: 1, Path: "/b"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := valid
			if Apply(&tree, tt.layout, tt.files, tt.tabOf) {
				t.Fatalf("Apply accepted %s", tt.name)
			}
			if tree.Root != valid.Root || tree.Focus != valid.Focus || *tree.Root != before {
				t.Fatalf("a rejected layout changed the tree: %+v", tree.Root)
			}
		})
	}
}

func TestSplitCaptureDeepTreeIsBounded(t *testing.T) {
	t.Parallel()
	// A tree beyond the depth bound never captures (and never recurses without end
	// on a hand-edited layout: Valid rejects it).
	node := &Node{Tab: 0}
	for i := 1; i < session.MaxSplitLeaves+2; i++ {
		node = &Node{Dir: Stacked, A: node, B: &Node{Tab: i}}
	}
	tree := Tree{Root: node, Focus: node.B}
	paths := make([]string, session.MaxSplitLeaves+3)
	for i := range paths {
		paths[i] = "/f"
	}
	if got := Capture(tree, TabState{Tab: 1}, filesOf(paths)); got != nil {
		t.Fatalf("over-limit tree captured %+v", got)
	}
}
