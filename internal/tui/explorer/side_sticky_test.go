package explorer

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	tree "github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/theme"
)

// scrollTree is a fakeTree with a movable scroll offset, for the sticky band
// tests.
type scrollTree struct {
	*fakeTree
	off int
}

func (f *scrollTree) ScrollOffset() int { return f.off }
func (f *scrollTree) ScrollBy(n int) {
	f.off = min(max(f.off+n, 0), max(len(f.rows)-1, 0))
}

// drawStyles is the dark chrome the drawing tests use, so style roles resolve
// against real theme keys.
func drawStyles(t *testing.T) style.Styles {
	t.Helper()
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	return style.New(th)
}

// deepRows is a tree of a/b/file... where rows[0] is the root, rows 1-2 are the
// folder chain, and rows 3+ are files under b: the Parent chain of a deep row is
// root, a, b.
func deepRows() []tree.Row {
	rows := []tree.Row{
		{Name: "root", Path: "/r", IsDir: true, Expanded: true, Parent: -1},
		{Name: "a", Path: "/r/a", IsDir: true, Expanded: true, Depth: 1, Parent: 0},
		{Name: "b", Path: "/r/a/b", IsDir: true, Expanded: true, Depth: 2, Parent: 1},
	}
	for i := range 9 {
		rows = append(rows, tree.Row{Name: "f" + string(rune('0'+i)), Path: "/r/a/b/f" + string(rune('0'+i)), Depth: 3, Parent: 2})
	}
	return rows
}

// TestCompactRowPaintsItsChain checks a compact row: the folded ancestors are
// drawn before the anchor's name, as "a/b/c", and the row keeps the anchor's
// indent (the folded ancestors do not indent).
func TestCompactRowPaintsItsChain(t *testing.T) {
	tr := &fakeTree{root: "/r", rows: []tree.Row{
		{Name: "r", Path: "/r", IsDir: true, Expanded: true, Parent: -1},
		{Name: "c", Path: "/r/a/b/c", IsDir: true, Expanded: false, Depth: 1, Parent: 0,
			Chain: []tree.ChainSegment{{Name: "a", Path: "/r/a"}, {Name: "b", Path: "/r/a/b"}}},
	}}
	p := New(tr, drawStyles(t), nil)
	g := Geometry{Top: 1, Width: 24, Bottom: 6}
	gr := grid.New(24, 6)
	p.Draw(gr, g)
	row := gr.Plain()[3]
	if !strings.Contains(row, "a/b/c") {
		t.Errorf("row = %q, want the compact chain a/b/c", row)
	}
	if got := gr.At(4, 3).Text; got == "" || got == " " {
		t.Errorf("row = %q: the chain renders at the anchor's depth, after the indicator at column 4", row)
	}
}

// TestStickyBandPinsTheRunFirstRowsAncestors checks the pinned band: at a run
// offset inside a deep branch, the ancestors are drawn above the run on the
// gutter ground, the run starts under them, and a press on a pinned row selects
// it.
func TestStickyBandPinsTheRunFirstRowsAncestors(t *testing.T) {
	tr := &scrollTree{fakeTree: &fakeTree{root: "/r", rows: deepRows()}, off: 5}
	st := drawStyles(t)
	p := New(tr, st, nil)
	g := Geometry{Top: 1, Width: 24, Bottom: 7} // five body rows
	gr := grid.New(24, 8)
	p.Draw(gr, g)
	rows := gr.Plain()
	// The body starts at row 2; pins are root, a, b, then the run from f3.
	want := []string{"root", "a", "b", "f2", "f3"}
	for i, name := range want {
		if !strings.Contains(rows[2+i], name) {
			t.Fatalf("body row %d = %q, want %q: pins root,a,b then the run from offset 5", i, rows[2+i], name)
		}
	}
	// The pinned band has the gutter ground, the run the panel ground. The cursor is
	// the pinned root, and a cursor keeps its own ground on a pinned row, as Fresh's
	// explorer does.
	gutter := style.Cell(st.Gutter)
	panel := style.Cell(st.Explorer)
	cursor := style.Cell(st.CurrentLine)
	if got := gr.At(3, 2).Style.BG; got != cursor.BG {
		t.Errorf("pinned cursor row ground = %v, want the current-line ground %v", got, cursor.BG)
	}
	if got := gr.At(3, 3).Style.BG; got != gutter.BG {
		t.Errorf("pinned row ground = %v, want the gutter ground %v", got, gutter.BG)
	}
	if got := gr.At(3, 5).Style.BG; got != panel.BG {
		t.Errorf("run row ground = %v, want the panel ground %v", got, panel.BG)
	}
	// A press on the pinned "a" row (body row 1) selects it.
	if _, _ = p.Update(click(3, 3), g); true {
		if sel, _ := tr.Selected(); sel.Name != "a" {
			t.Errorf("a press on the pinned row selected %q, want a", sel.Name)
		}
	}
}

// TestRightSidePanelDrawsInItsColumns checks the right-hand placement: the panel
// paints its walls in its own columns, its grip is the editor-facing left border,
// and a grip drag widens it as the pointer moves left.
func TestRightSidePanelDrawsInItsColumns(t *testing.T) {
	tr := &fakeTree{root: "/r", rows: []tree.Row{
		{Name: "r", Path: "/r", IsDir: true, Expanded: true, Parent: -1},
		{Name: "a.txt", Path: "/r/a.txt", Depth: 1, Parent: 0},
	}}
	p := New(tr, drawStyles(t), nil)
	g := Geometry{X: 14, Width: 10, Top: 1, Bottom: 6, Right: true}
	gr := grid.New(24, 8)
	p.Draw(gr, g)
	if got := gr.At(14, 2).Text; got != "│" {
		t.Errorf("left wall at 14 = %q, want │", got)
	}
	if got := gr.At(23, 2).Text; got != "│" {
		t.Errorf("right wall at 23 = %q, want │", got)
	}
	if got := gr.At(13, 2).Text; got != " " {
		t.Errorf("cell left of the panel = %q, want the editor's ground", got)
	}
	// The grip is the left border on this side: press, then move left to widen.
	p, _ = p.Update(click(14, 3), g)
	_, cmd := p.Update(tea.MouseMotionMsg(tea.Mouse{X: 12, Y: 3, Button: tea.MouseLeft}), g)
	if got := run(cmd); got != (ResizeMsg{Width: 12}) {
		t.Errorf("right-side grip drag sent %+v, want width 12 (14+10-12)", got)
	}
	// A press outside the panel is not the panel's.
	if _, cmd := p.Update(click(13, 3), g); cmd != nil {
		t.Errorf("a press left of a right-side panel sent %+v", run(cmd))
	}
}
