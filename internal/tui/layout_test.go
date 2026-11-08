package tui

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/grid"
)

// The numbers below are the blueprint section 4 values at the sample widths. The
// expectations come from the legacy internal/tui layout tests, which these
// rules were ported from.
var sampleWidths = []struct {
	w        int
	explorer int // E, section 4.2
}{
	{w: 40, explorer: 20}, // the editor keeps 20 columns
	{w: 60, explorer: 32}, // the default width
	{w: 80, explorer: 32},
	{w: 100, explorer: 32},
	{w: 140, explorer: 32},
}

var sampleHeights = []int{15, 20, 24, 30, 40}

// layoutFor is frame.ComputeLayout with a readable name for the tests.
func layoutFor(w, h int, in layoutInput) layout {
	return frame.ComputeLayout(w, h, in)
}

// paintRow paints the runs on a w-wide row and returns the row's text.
func paintRow(w int, runs []run) string {
	g := grid.New(w, 1)
	for _, r := range runs {
		g.Put(r.X, 0, w, r.Text, grid.Style{})
	}
	return g.Plain()[0]
}

func TestExplorerWidthAtSampleWidths(t *testing.T) {
	for _, g := range sampleWidths {
		if got := frame.ExplorerCols(g.w, 0); got != g.explorer {
			t.Errorf("W=%d: E = %d, want %d", g.w, got, g.explorer)
		}
	}
}

func TestExplorerOverrideIsClamped(t *testing.T) {
	tests := []struct{ w, override, want int }{
		{100, 38, 38},  // the M23 drag: 29 to 38
		{100, 2, 16},   // below the floor
		{100, 200, 60}, // above the ceiling
		{40, 200, 20},  // the upper bound is W-20
		{10, 8, 10},    // a frame narrower than the floor keeps its width
	}
	for _, tt := range tests {
		if got := frame.ExplorerCols(tt.w, tt.override); got != tt.want {
			t.Errorf("frame.ExplorerCols(%d, %d) = %d, want %d", tt.w, tt.override, got, tt.want)
		}
	}
}

func TestEditorColumnsAtSampleWidths(t *testing.T) {
	for _, g := range sampleWidths {
		l := layoutFor(g.w, 30, layoutInput{ExplorerOpen: true, Lines: 4})
		if l.EditorX != g.explorer {
			t.Errorf("W=%d: editorX = %d, want E = %d", g.w, l.EditorX, g.explorer)
		}
		// The gutter is six cells at one-digit line counts: "  1 │ ".
		if l.Gutter != 6 || l.TextX != g.explorer+6 {
			t.Errorf("W=%d: gutter %d at textX %d", g.w, l.Gutter, l.TextX)
		}
		if want := g.w - g.explorer - 6; l.TextW != want {
			t.Errorf("W=%d: textW = %d, want %d", g.w, l.TextW, want)
		}
	}
}

func TestGutterWidthWidensWithDigits(t *testing.T) {
	// M09 rule: "  88 │" at 200 lines, so three digits widen the gutter to 7.
	tests := map[int]int{1: 6, 9: 6, 10: 6, 99: 6, 100: 7, 200: 7, 1000: 8}
	for lines, want := range tests {
		if got := frame.GutterWidth(lines); got != want {
			t.Errorf("frame.GutterWidth(%d) = %d, want %d", lines, got, want)
		}
	}
}

func TestRowBandsAtSampleHeights(t *testing.T) {
	for _, h := range sampleHeights {
		l := layoutFor(100, h, layoutInput{})
		if l.BodyTop != 3 || l.BodyBottom != h-2 || l.StatusY != h-1 {
			t.Errorf("H=%d: body %d..%d status %d", h, l.BodyTop, l.BodyBottom, l.StatusY)
		}
	}
}

func TestScrollbarShowsOnlyWhenContentOverflows(t *testing.T) {
	const h = 15 // body rows 3..13, so 11 rows
	if layoutFor(80, h, layoutInput{Lines: 11}).Scrollbar {
		t.Error("11 lines in 11 rows show a scrollbar")
	}
	if !layoutFor(80, h, layoutInput{Lines: 12}).Scrollbar {
		t.Error("12 lines in 11 rows do not show a scrollbar")
	}
}

func TestExplorerTitleButtonsFollowTheTitle(t *testing.T) {
	// At E=18 (W=60) the reload button shows and the close button does not.
	tests := []struct {
		e, reload, closeX int
	}{
		{16, -1, -1},
		{17, 16, -1},
		{18, 16, -1},
		{20, 16, 18},
		{30, 25, 27},
	}
	for _, tt := range tests {
		l := layoutFor(100, 30, layoutInput{ExplorerOpen: true, ExplorerOverride: tt.e})
		if l.Ex != tt.e || l.ExReloadX != tt.reload || l.ExCloseX != tt.closeX {
			t.Errorf("E=%d: buttons at %d and %d (E %d), want %d and %d",
				tt.e, l.ExReloadX, l.ExCloseX, l.Ex, tt.reload, tt.closeX)
		}
	}
}

func TestExplorerHiddenHasNoColumns(t *testing.T) {
	l := layoutFor(100, 30, layoutInput{})
	if l.ExplorerOpen || l.Ex != 0 || l.ExReloadX != -1 || l.ExCloseX != -1 {
		t.Fatalf("hidden explorer: open %v ex %d buttons %d and %d",
			l.ExplorerOpen, l.Ex, l.ExReloadX, l.ExCloseX)
	}
	if l.EditorX != 0 {
		t.Fatalf("editorX = %d with the explorer hidden", l.EditorX)
	}
}

func TestTabStripStartsAfterTheExplorer(t *testing.T) {
	l := layoutFor(100, 30, layoutInput{ExplorerOpen: true, Tabs: []TabInfo{{Name: "a.txt"}}})
	if got := l.Strip.Runs[0]; got.X != 32 || got.Text != " a.txt " {
		t.Fatalf("first tab run = %+v", got)
	}
}

func TestTabStripOverflowsNarrowRows(t *testing.T) {
	tabs := []TabInfo{{Name: "one.go"}, {Name: "two.go"}, {Name: "three.go"}, {Name: "four.go"}}
	wide := layoutFor(100, 30, layoutInput{Tabs: tabs})
	if wide.Strip.Overflow {
		t.Error("four tabs at W=100 overflow")
	}
	narrow := layoutFor(20, 30, layoutInput{Tabs: tabs})
	if !narrow.Strip.Overflow {
		t.Fatal("four tabs at W=20 do not overflow")
	}
	for _, r := range narrow.Strip.Runs {
		if r.Kind == hitTabNew || r.Kind == hitTabScrollBefore || r.Kind == hitTabScrollAfter {
			continue
		}
		if r.X < 1 || r.X+r.W > 19-3 {
			t.Errorf("run %+v is outside the overflow window left of the + button", r)
		}
	}
}

func TestMenuLabelsJoinWhileExplorerHasFocus(t *testing.T) {
	plain := paintRow(80, frame.MenuRuns(false))
	if strings.Contains(plain, "Explorer") {
		t.Errorf("menu without focus shows Explorer: %q", plain)
	}
	focused := paintRow(80, frame.MenuRuns(true))
	if !strings.Contains(focused, "Explorer") || !strings.HasPrefix(focused, " File") {
		t.Errorf("menu with focus = %q", focused)
	}
}

func TestGateReplacesTheWholeFrame(t *testing.T) {
	gated := []struct{ w, h int }{{7, 10}, {8, 2}, {1, 1}}
	for _, c := range gated {
		if l := layoutFor(c.w, c.h, layoutInput{}); !l.Gated {
			t.Errorf("%dx%d is not gated", c.w, c.h)
		}
	}
	if l := layoutFor(8, 3, layoutInput{}); l.Gated {
		t.Fatal("8x3 is gated, but it is a sample size above the gate")
	}
	if got := layoutFor(3, 1, layoutInput{}).Hit(0, 0); got.Kind != hitNone {
		t.Fatalf("hit under the gate = %+v", got)
	}
}

// statusRow paints the status row alone on a w-wide row, as the status bar does.
func statusRow(w int, s statusView) string {
	return paintRow(w, layoutFor(w, 3, layoutInput{Status: s}).Status)
}

func TestStatusRightBlockAtBlueprintWidths(t *testing.T) {
	// Blueprint section 4.5 hand checks with a warning counter present.
	s := statusView{Position: "Ln 1, Col 1", EOL: "LF", Encoding: "ASCII", Language: "Text", Warnings: 2, Lines: 4}
	tests := []struct {
		w     int
		right string // the right block, from its first cell to the edge
	}{
		{40, "LF  ASCII  Text "},
		{41, "LF  ASCII  Text  [⚠ 2] "},
		{60, "LF  ASCII  Text  [⚠ 2] "},
		{70, "LF  ASCII  Text  [⚠ 2]  Palette: Ctrl+P "},
	}
	for _, tt := range tests {
		row := statusRow(tt.w, s)
		if !strings.HasSuffix(row, tt.right) {
			t.Errorf("W=%d: row %q does not end with %q", tt.w, row, tt.right)
		}
	}
}

func TestStatusLeftGroupKeepsItsPosition(t *testing.T) {
	s := statusView{Position: "Ln 3, Col 7", Message: "Saved", Lines: 4, EOL: "LF"}
	row := statusRow(100, s)
	if !strings.HasPrefix(row, " Local  Ln 3, Col 7") {
		t.Errorf("status row = %q", row)
	}
	if !strings.Contains(row, " Saved ") {
		t.Errorf("status message missing: %q", row)
	}
}

func TestStatusPaletteSegmentIsClickable(t *testing.T) {
	l := layoutFor(100, 30, layoutInput{Status: statusView{Position: "Ln 1, Col 1", Lines: 1}})
	last := l.Status[len(l.Status)-1]
	if last.Kind != hitStatusPalette {
		t.Fatalf("last status run kind = %v", last.Kind)
	}
	if got := l.Hit(last.X, l.StatusY); got.Kind != hitStatusPalette {
		t.Errorf("hit on palette segment = %+v", got)
	}
}

func TestPositionWidthGrowsWithLineCount(t *testing.T) {
	if got := frame.PositionWidth(1); got != 13 {
		t.Errorf("frame.PositionWidth(1) = %d, want 13", got)
	}
	if got := frame.PositionWidth(100); got != 15 {
		t.Errorf("frame.PositionWidth(100) = %d, want 15", got)
	}
}
