package explorer

import (
	"strings"
	"testing"

	tree "github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
)

func TestLongNamesEndWithAnEllipsisAndKeepTheExtension(t *testing.T) {
	t.Parallel()
	ft := &fakeTree{
		root: "/r",
		rows: []tree.Row{
			{Name: "r", Path: "/r", IsDir: true, Expanded: true},
			{Name: "file_open_orchestrator.rs", Path: "/r/file_open_orchestrator.rs", Depth: 1},
			{Name: "file_open_input.rs", Path: "/r/file_open_input.rs", Depth: 1},
			{Name: "wave-animation-wireframe-notes", Path: "/r/wave-animation-wireframe-notes", Depth: 1},
		},
	}
	g := grid.New(28, 8)
	New(ft, style.Styles{}, nil).Draw(g, Geometry{Width: 28, Bottom: 7})
	lines := g.Plain()
	// Width 28: the name starts at column 6 and ends at column 25, so it has 20 cells.
	if got := lines[3]; !strings.Contains(got, "file_open_orches….rs") {
		t.Errorf("long file row %q: want the stem cut with an ellipsis and the extension kept", got)
	}
	if got := lines[4]; !strings.Contains(got, "file_open_input.rs") {
		t.Errorf("fitting file row %q: want the whole name", got)
	}
	if got := lines[5]; !strings.Contains(got, "wave-animation-wire…") {
		t.Errorf("long name without an extension %q: want an ellipsis at the end", got)
	}
	// The padding cell before the border stays blank.
	for y := 3; y <= 5; y++ {
		if c := g.At(26, y).Text; c != " " {
			t.Errorf("row %d: padding cell before the border holds %q", y, c)
		}
	}
}

func TestFirstAndLastActionsMoveTheCursorToTheEdges(t *testing.T) {
	t.Parallel()
	p := New(opsTree(1), style.Styles{}, nil)
	p, _ = doAction(t, p, "file_explorer_last")
	if sel, _ := p.tree.Selected(); sel.Name != "b.txt" {
		t.Errorf("last selected %q, want b.txt", sel.Name)
	}
	p, _ = doAction(t, p, "file_explorer_first")
	if sel, _ := p.tree.Selected(); sel.Name != "a" {
		t.Errorf("first selected %q, want a", sel.Name)
	}
}

func TestDrawNeverPanicsAtAnyWidth(t *testing.T) {
	t.Parallel()
	ft := &fakeTree{
		root: "/r",
		rows: []tree.Row{
			{Name: "r", Path: "/r", IsDir: true, Expanded: true},
			{Name: "a_long_file_name.go", Path: "/r/a_long_file_name.go", Depth: 3},
		},
	}
	for w := 0; w <= 40; w++ {
		for _, focus := range []bool{true, false} {
			g := grid.New(max(w, 1), 6)
			New(ft, style.Styles{}, nil).Draw(g, Geometry{Width: w, Bottom: 5, Focused: focus, Marks: map[string]rune{"/r/a_long_file_name.go": 'M'}})
		}
	}
}
