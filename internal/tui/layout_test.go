package tui

import (
	"strings"
	"testing"

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

// layoutFor is computeLayout with a readable name for the tests.
func layoutFor(w, h int, in layoutInput) layout {
	return computeLayout(w, h, in)
}

// paintRow paints the runs on a w-wide row and returns the row's text.
func paintRow(w int, runs []run) string {
	g := grid.New(w, 1)
	for _, r := range runs {
		g.Put(r.x, 0, w, r.text, grid.Style{})
	}
	return g.Plain()[0]
}

func TestExplorerWidthAtSampleWidths(t *testing.T) {
	for _, g := range sampleWidths {
		if got := explorerCols(g.w, 0); got != g.explorer {
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
		if got := explorerCols(tt.w, tt.override); got != tt.want {
			t.Errorf("explorerCols(%d, %d) = %d, want %d", tt.w, tt.override, got, tt.want)
		}
	}
}

func TestEditorColumnsAtSampleWidths(t *testing.T) {
	for _, g := range sampleWidths {
		l := layoutFor(g.w, 30, layoutInput{explorerOpen: true, lines: 4})
		if l.editorX != g.explorer {
			t.Errorf("W=%d: editorX = %d, want E = %d", g.w, l.editorX, g.explorer)
		}
		// The gutter is six cells at one-digit line counts: "  1 │ ".
		if l.gutter != 6 || l.textX != g.explorer+6 {
			t.Errorf("W=%d: gutter %d at textX %d", g.w, l.gutter, l.textX)
		}
		if want := g.w - g.explorer - 6; l.textW != want {
			t.Errorf("W=%d: textW = %d, want %d", g.w, l.textW, want)
		}
	}
}

func TestGutterWidthWidensWithDigits(t *testing.T) {
	// M09 rule: "  88 │" at 200 lines, so three digits widen the gutter to 7.
	tests := map[int]int{1: 6, 9: 6, 10: 6, 99: 6, 100: 7, 200: 7, 1000: 8}
	for lines, want := range tests {
		if got := gutterWidth(lines); got != want {
			t.Errorf("gutterWidth(%d) = %d, want %d", lines, got, want)
		}
	}
}

func TestRowBandsAtSampleHeights(t *testing.T) {
	for _, h := range sampleHeights {
		l := layoutFor(100, h, layoutInput{})
		if l.bodyTop != 2 || l.bodyBottom != h-2 || l.statusY != h-1 {
			t.Errorf("H=%d: body %d..%d status %d", h, l.bodyTop, l.bodyBottom, l.statusY)
		}
	}
}

func TestScrollbarShowsOnlyWhenContentOverflows(t *testing.T) {
	const h = 15 // body rows 2..13, so 12 rows
	if layoutFor(80, h, layoutInput{lines: 12}).scrollbar {
		t.Error("12 lines in 12 rows show a scrollbar")
	}
	if !layoutFor(80, h, layoutInput{lines: 13}).scrollbar {
		t.Error("13 lines in 12 rows do not show a scrollbar")
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
		l := layoutFor(100, 30, layoutInput{explorerOpen: true, explorerOverride: tt.e})
		if l.ex != tt.e || l.exReloadX != tt.reload || l.exCloseX != tt.closeX {
			t.Errorf("E=%d: buttons at %d and %d (E %d), want %d and %d",
				tt.e, l.exReloadX, l.exCloseX, l.ex, tt.reload, tt.closeX)
		}
	}
}

func TestExplorerHiddenHasNoColumns(t *testing.T) {
	l := layoutFor(100, 30, layoutInput{})
	if l.explorerOpen || l.ex != 0 || l.exReloadX != -1 || l.exCloseX != -1 {
		t.Fatalf("hidden explorer: open %v ex %d buttons %d and %d",
			l.explorerOpen, l.ex, l.exReloadX, l.exCloseX)
	}
	if l.editorX != 0 {
		t.Fatalf("editorX = %d with the explorer hidden", l.editorX)
	}
}

func TestTabStripStartsAfterTheExplorer(t *testing.T) {
	l := layoutFor(100, 30, layoutInput{explorerOpen: true, tabs: []TabInfo{{Name: "a.txt"}}})
	if got := l.strip.runs[0]; got.x != 32 || got.text != " a.txt " {
		t.Fatalf("first tab run = %+v", got)
	}
}

func TestTabStripOverflowsNarrowRows(t *testing.T) {
	tabs := []TabInfo{{Name: "one.go"}, {Name: "two.go"}, {Name: "three.go"}, {Name: "four.go"}}
	wide := layoutFor(100, 30, layoutInput{tabs: tabs})
	if wide.strip.overflow {
		t.Error("four tabs at W=100 overflow")
	}
	narrow := layoutFor(20, 30, layoutInput{tabs: tabs})
	if !narrow.strip.overflow {
		t.Fatal("four tabs at W=20 do not overflow")
	}
	for _, r := range narrow.strip.runs {
		if r.kind == hitTabNew || r.kind == hitTabScrollBefore || r.kind == hitTabScrollAfter {
			continue
		}
		if r.x < 1 || r.x+r.w > 19-1 {
			t.Errorf("run %+v is outside the overflow window", r)
		}
	}
}

func TestMenuLabelsJoinWhileExplorerHasFocus(t *testing.T) {
	plain := paintRow(80, menuRuns(false))
	if strings.Contains(plain, "Explorer") {
		t.Errorf("menu without focus shows Explorer: %q", plain)
	}
	focused := paintRow(80, menuRuns(true))
	if !strings.Contains(focused, "Explorer") || !strings.HasPrefix(focused, " File") {
		t.Errorf("menu with focus = %q", focused)
	}
}

func TestGateReplacesTheWholeFrame(t *testing.T) {
	gated := []struct{ w, h int }{{7, 10}, {8, 2}, {1, 1}}
	for _, c := range gated {
		if l := layoutFor(c.w, c.h, layoutInput{}); !l.gated {
			t.Errorf("%dx%d is not gated", c.w, c.h)
		}
	}
	if l := layoutFor(8, 3, layoutInput{}); l.gated {
		t.Fatal("8x3 is gated, but it is a sample size above the gate")
	}
	if got := layoutFor(3, 1, layoutInput{}).hit(0, 0); got.Kind != hitNone {
		t.Fatalf("hit under the gate = %+v", got)
	}
}

// statusRow paints the status row alone on a w-wide row, as the status bar does.
func statusRow(w int, s statusView) string {
	return paintRow(w, layoutFor(w, 3, layoutInput{status: s}).status)
}

func TestStatusRightBlockAtBlueprintWidths(t *testing.T) {
	// Blueprint section 4.5 hand checks with a warning counter present.
	s := statusView{position: "Ln 1, Col 1", eol: "LF", encoding: "ASCII", language: "Text", warnings: 2, lines: 4}
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
	s := statusView{position: "Ln 3, Col 7", message: "Saved", lines: 4, eol: "LF"}
	row := statusRow(100, s)
	if !strings.HasPrefix(row, " Local  Ln 3, Col 7") {
		t.Errorf("status row = %q", row)
	}
	if !strings.Contains(row, " Saved ") {
		t.Errorf("status message missing: %q", row)
	}
}

func TestStatusPaletteSegmentIsClickable(t *testing.T) {
	l := layoutFor(100, 30, layoutInput{status: statusView{position: "Ln 1, Col 1", lines: 1}})
	last := l.status[len(l.status)-1]
	if last.kind != hitStatusPalette {
		t.Fatalf("last status run kind = %v", last.kind)
	}
	if got := l.hit(last.x, l.statusY); got.Kind != hitStatusPalette {
		t.Errorf("hit on palette segment = %+v", got)
	}
}

func TestPositionWidthGrowsWithLineCount(t *testing.T) {
	if got := positionWidth(1); got != 13 {
		t.Errorf("positionWidth(1) = %d, want 13", got)
	}
	if got := positionWidth(100); got != 15 {
		t.Errorf("positionWidth(100) = %d, want 15", got)
	}
}
