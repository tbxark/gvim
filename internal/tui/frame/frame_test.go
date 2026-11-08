package frame

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/icons"
)

// stripTabs are two tabs for the strip tests.
func stripTabs() []TabInfo {
	return []TabInfo{{Name: "a.txt", Active: true}, {Name: "b.txt"}}
}

// TestMenuBandTakesTwoRows checks the menu band: the labels on row 0, one
// padding row under them, the tab strip on row 2 and the body from row 3.
func TestMenuBandTakesTwoRows(t *testing.T) {
	l := ComputeLayout(100, 30, Input{Tabs: stripTabs()})
	if !l.MenuOn || l.MenuTop != 0 || l.MenuBottom != MenuRows-1 {
		t.Fatalf("menu band on=%v at %d..%d, want on at 0..%d", l.MenuOn, l.MenuTop, l.MenuBottom, MenuRows-1)
	}
	if l.MenuLabelY != 0 {
		t.Fatalf("label row = %d, want 0", l.MenuLabelY)
	}
	if l.StripY != 2 || l.BodyTop != 3 {
		t.Fatalf("strip row %d, body top %d; want 2 and 3", l.StripY, l.BodyTop)
	}
	if got := l.Hit(1, l.MenuLabelY); got.Kind != HitMenuLabel {
		t.Errorf("the label row at File hits %v, want a menu label", got.Kind)
	}
	if got := l.Hit(1, l.MenuBottom); got.Kind != HitNone {
		t.Errorf("the padding row of the band hits %v, want nothing", got.Kind)
	}
	if got := l.Hit(1, l.StripY); got.Kind != HitTab {
		t.Errorf("row 2 at the first tab hits %v, want the tab", got.Kind)
	}
	if got := l.Hit(l.TextX, l.BodyTop); got.Kind != HitEditorText {
		t.Errorf("row 3 at the text hits %v, want the editor text", got.Kind)
	}
}

// TestHiddenMenuBarKeepsTheOldRows checks the rows with the menu bar hidden: the
// strip on row 0, the body from row 1 and the explorer title on row 1, as before
// the band existed.
func TestHiddenMenuBarKeepsTheOldRows(t *testing.T) {
	l := ComputeLayout(100, 30, Input{HideMenuBar: true, ExplorerOpen: true, ExplorerRows: 3, Tabs: stripTabs()})
	if l.MenuOn || l.MenuTop != -1 || l.MenuBottom != -1 {
		t.Fatalf("menu band on=%v at %d..%d with the bar hidden", l.MenuOn, l.MenuTop, l.MenuBottom)
	}
	if l.StripY != 0 || l.BodyTop != 1 {
		t.Fatalf("strip row %d, body top %d; want 0 and 1", l.StripY, l.BodyTop)
	}
	if l.ExTitleY != 1 || l.ExBodyTop != 2 {
		t.Fatalf("explorer title row %d body top %d, want 1 and 2", l.ExTitleY, l.ExBodyTop)
	}
	if got := l.Hit(l.EditorX+1, 0); got.Kind != HitTab {
		t.Errorf("row 0 at the first tab hits %v, want the tab", got.Kind)
	}
	if got := l.Hit(3, 1); got.Kind != HitExplorerBody {
		t.Errorf("row 1 in the explorer title hits %v, want the title body", got.Kind)
	}
	if got := l.Hit(3, 2); got.Kind != HitExplorerRow || got.Index != 0 {
		t.Errorf("row 2 hits %+v, want the first tree row", got)
	}
}

// TestExplorerTitleAlignsWithTheStrip checks that the explorer's title row is the
// tab strip row when the menu bar shows, and the tree body starts under it.
func TestExplorerTitleAlignsWithTheStrip(t *testing.T) {
	l := ComputeLayout(100, 30, Input{ExplorerOpen: true, ExplorerRows: 3, Tabs: stripTabs()})
	if l.ExTitleY != l.StripY {
		t.Fatalf("explorer title row %d, strip row %d; want the same row", l.ExTitleY, l.StripY)
	}
	if l.ExTitleY != 2 || l.ExBodyTop != 3 {
		t.Fatalf("explorer title row %d body top %d, want 2 and 3", l.ExTitleY, l.ExBodyTop)
	}
	if got := l.Hit(3, l.ExTitleY); got.Kind != HitExplorerBody {
		t.Errorf("the title row hits %v, want the title body", got.Kind)
	}
	if got := l.Hit(3, l.ExBodyTop); got.Kind != HitExplorerRow || got.Index != 0 {
		t.Errorf("the first tree row hits %+v", got)
	}
	if got := l.Hit(3, 1); got.Kind != HitNone {
		t.Errorf("the menu padding row over the explorer hits %v, want nothing", got.Kind)
	}
}

// TestNewTabButtonIsThreeCellsAtTheRightEdge checks the " + " button: three cells
// ending on the last column, one blank on each side of the glyph, and a hit target
// that covers all three cells.
func TestNewTabButtonIsThreeCellsAtTheRightEdge(t *testing.T) {
	for _, mode := range []icons.Mode{icons.Plain, icons.Nerd} {
		const w = 100
		l := ComputeLayout(w, 30, Input{Tabs: stripTabs(), IconMode: mode})
		last := l.Strip.Runs[len(l.Strip.Runs)-1]
		if want := " " + mode.Glyph(icons.New) + " "; last.Kind != HitTabNew || last.X != w-3 || last.W != 3 || last.Text != want {
			t.Fatalf("%v: + button = %+v, want %q at %d..%d", mode, last, want, w-3, w-1)
		}
		for x := w - 3; x < w; x++ {
			if got := l.Hit(x, l.StripY); got.Kind != HitTabNew {
				t.Errorf("%v: column %d hits %v, want the new-tab button", mode, x, got.Kind)
			}
		}
		if got := l.Hit(w-4, l.StripY); got.Kind == HitTabNew {
			t.Errorf("%v: the cell left of the button hits it", mode)
		}
	}
}

// TestNewTabButtonNeedsThreeCells checks the narrow frames: the button is drawn
// only when the row still has three cells, and it is drawn whole.
func TestNewTabButtonNeedsThreeCells(t *testing.T) {
	for w := 0; w < 3; w++ {
		if s := PlaceStrip(0, w, nil, 0, icons.Plain); len(s.Runs) != 0 {
			t.Errorf("a %d-cell strip draws %+v, want no button", w, s.Runs)
		}
	}
	s := PlaceStrip(0, 3, nil, 0, icons.Plain)
	if len(s.Runs) != 1 || s.Runs[0].Kind != HitTabNew || s.Runs[0].X != 0 || s.Runs[0].W != 3 {
		t.Fatalf("a three-cell strip = %+v, want the whole button", s.Runs)
	}
	// A strip that starts past the button's column has no button.
	if s := PlaceStrip(2, 5, nil, 0, icons.Plain); len(s.Runs) != 1 || s.Runs[0].X != 2 || s.Runs[0].W != 3 {
		t.Fatalf("PlaceStrip(2, 5) = %+v, want the button at columns 2..4", s.Runs)
	}
	if s := PlaceStrip(2, 4, nil, 0, icons.Plain); len(s.Runs) != 0 {
		t.Fatalf("PlaceStrip(2, 4) = %+v, want no room for the button", s.Runs)
	}
}

// TestOverflowWindowEndsBeforeTheButton checks that the tabs and the ">" marker
// stay left of the three-cell button.
func TestOverflowWindowEndsBeforeTheButton(t *testing.T) {
	tabs := []TabInfo{{Name: "one.go"}, {Name: "two.go"}, {Name: "three.go"}, {Name: "four.go"}}
	const w = 20
	l := ComputeLayout(w, 30, Input{Tabs: tabs})
	if !l.Strip.Overflow {
		t.Fatal("four tabs at 20 columns do not overflow")
	}
	if last := l.Strip.Runs[len(l.Strip.Runs)-1]; last.Kind != HitTabNew || last.X != w-3 {
		t.Fatalf("+ button = %+v, want it at %d", last, w-3)
	}
	for _, r := range l.Strip.Runs {
		switch r.Kind {
		case HitTabNew:
			continue
		case HitTabScrollBefore:
			if r.X != 0 {
				t.Errorf("left marker at %d, want 0", r.X)
			}
		case HitTabScrollAfter:
			if r.X != w-4 {
				t.Errorf("right marker at %d, want %d, one cell left of the button", r.X, w-4)
			}
		default:
			if r.X < 1 || r.X+r.W > w-4 {
				t.Errorf("tab run %+v is outside the window left of the button", r)
			}
		}
	}
}

// TestStatusRowKeepsEdgePadding checks the status bar edges: the left group starts
// with a blank cell and the palette hint ends with one before the right edge.
func TestStatusRowKeepsEdgePadding(t *testing.T) {
	const w = 100
	l := ComputeLayout(w, 30, Input{Status: StatusView{Position: "Ln 1, Col 1", Lines: 1, EOL: "LF"}})
	if len(l.Status) == 0 {
		t.Fatal("the status row has no runs")
	}
	first, last := l.Status[0], l.Status[len(l.Status)-1]
	if first.X != 0 || !strings.HasPrefix(first.Text, " ") {
		t.Errorf("the left group starts at %d with %q, want a blank cell at column 0", first.X, first.Text)
	}
	if last.Kind != HitStatusPalette {
		t.Errorf("the last run is %v, want the palette hint", last.Kind)
	}
	if last.X+last.W != w || !strings.HasSuffix(last.Text, " ") {
		t.Errorf("the palette hint ends at %d with %q, want a blank cell before column %d", last.X+last.W, last.Text, w)
	}
}

// TestStatusRowIsTheLastRowWithoutAPrompt checks the prompt row rule at the frame
// level: the status row is the last row unless a prompt row is asked for.
func TestStatusRowIsTheLastRowWithoutAPrompt(t *testing.T) {
	idle := ComputeLayout(80, 24, Input{})
	if !idle.StatusOn || idle.StatusY != 23 || idle.PromptOn {
		t.Fatalf("idle: status row %d on=%v prompt=%v, want row 23 and no prompt", idle.StatusY, idle.StatusOn, idle.PromptOn)
	}
	asked := ComputeLayout(80, 24, Input{ShowPromptLine: true})
	if !asked.PromptOn || asked.PromptY != 23 || asked.StatusY != 22 {
		t.Fatalf("prompt row %d on=%v, status %d; want the prompt on row 23 above the status row 22",
			asked.PromptY, asked.PromptOn, asked.StatusY)
	}
}
