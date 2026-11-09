package explorer

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	tree "github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui/style"
)

// dragTree is a project with a/ holding x.txt and y.txt, and b/ at the root.
func dragTree() *fakeTree {
	return &fakeTree{
		root: "/r",
		rows: []tree.Row{
			{Name: "a", Path: "/r/a", IsDir: true, Depth: 0},
			{Name: "x.txt", Path: "/r/a/x.txt", Depth: 1},
			{Name: "y.txt", Path: "/r/a/y.txt", Depth: 1},
			{Name: "b", Path: "/r/b", IsDir: true, Depth: 0},
		},
		sel: 1,
	}
}

// dragGeometry is a panel with a title row and enough body rows for dragTree.
func dragGeometry() Geometry {
	return Geometry{Top: 1, Width: 16, Bottom: 10, Focused: true}
}

// dragRowY is the screen row of a row index (the title row is row 0).
func dragRowY(row int) int { return 1 + 1 + row }

// dragMotion and dragRelease are a left-button motion and release at (x, y).
func dragMotion(x, y int) tea.MouseMotionMsg {
	return tea.MouseMotionMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft})
}
func dragRelease(x, y int) tea.MouseReleaseMsg {
	return tea.MouseReleaseMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft})
}

// TestDragDropLiftsTheSelectionOntoADirectory checks the panel's move gesture: press a
// file row, drag over a directory row and release emits one MoveMsg with the effective
// selection and the target directory; the press/motion emit no move.
func TestDragDropLiftsTheSelectionOntoADirectory(t *testing.T) {
	p := New(dragTree(), style.Styles{}, nil)
	g := dragGeometry()
	p, _ = p.Update(click(5, dragRowY(1)), g) // press x.txt
	p, cmd := p.Update(dragMotion(5, dragRowY(3)), g)
	if cmd != nil {
		t.Fatalf("the drag motion already sent %#v", run(cmd))
	}
	p, cmd = p.Update(dragRelease(5, dragRowY(3)), g) // release over b/
	got, _ := run(cmd).(MoveMsg)
	if got.Dir != "/r/b" || len(got.Paths) != 1 || got.Paths[0] != "/r/a/x.txt" {
		t.Fatalf("move message = %#v", got)
	}
	if p.dragActive || p.dragRows != nil {
		t.Fatal("the panel kept its drag state after the release")
	}
}

// TestDragDropFiltersPointlessMoves checks a drop that would not move anything (the
// source's own parent, the source itself or a path inside a dragged directory) sends
// nothing and leaves no highlight.
func TestDragDropFiltersPointlessMoves(t *testing.T) {
	cases := map[string]struct {
		press, drop int
	}{
		"same parent": {1, 0},
		"self/inside": {0, 2},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p := New(dragTree(), style.Styles{}, nil)
			g := dragGeometry()
			p, _ = p.Update(click(5, dragRowY(tc.press)), g)
			p, _ = p.Update(dragMotion(5, dragRowY(tc.drop)), g)
			p, cmd := p.Update(dragRelease(5, dragRowY(tc.drop)), g)
			if cmd != nil {
				t.Fatalf("a pointless drop sent %#v", run(cmd))
			}
			if p.dragActive || p.dropDir != "" {
				t.Fatal("a pointless drop kept its drag highlight")
			}
		})
	}
}

// TestDragDropMovesTheWholeEffectiveSelection checks dragging one of several picked
// rows moves the whole selection, as Fresh does.
func TestDragDropMovesTheWholeEffectiveSelection(t *testing.T) {
	p := New(dragTree(), style.Styles{}, nil)
	p.st.picked = map[string]bool{"/r/a/x.txt": true, "/r/a/y.txt": true}
	g := dragGeometry()
	p, _ = p.Update(click(5, dragRowY(1)), g) // press x.txt, which is part of the selection
	p, _ = p.Update(dragMotion(5, dragRowY(3)), g)
	p, cmd := p.Update(dragRelease(5, dragRowY(3)), g)
	got, _ := run(cmd).(MoveMsg)
	if got.Dir != "/r/b" || len(got.Paths) != 2 {
		t.Fatalf("move message = %#v, want both picked files", got)
	}
}

// TestDragWithoutMotionSendsNoMove checks a plain click still behaves as a click: a
// release with no motion never moves.
func TestDragWithoutMotionSendsNoMove(t *testing.T) {
	p := New(dragTree(), style.Styles{}, nil)
	g := dragGeometry()
	p, _ = p.Update(click(5, dragRowY(1)), g)
	_, cmd := p.Update(dragRelease(5, dragRowY(1)), g)
	if cmd != nil {
		t.Fatalf("a click released as a move: %#v", run(cmd))
	}
}
