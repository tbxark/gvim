package frame

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

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

// TestDiagCountsText checks the badge text against Fresh's diagnostics element:
// errors, then warnings, then info and hints together, each part only when its
// count is above zero.
func TestDiagCountsText(t *testing.T) {
	tests := []struct {
		c    DiagCounts
		want string
	}{
		{DiagCounts{}, ""},
		{DiagCounts{Errors: 1}, "E:1"},
		{DiagCounts{Warnings: 2}, "W:2"},
		{DiagCounts{Info: 3}, "I:3"},
		{DiagCounts{Errors: 1, Warnings: 2}, "E:1 W:2"},
		{DiagCounts{Errors: 1, Warnings: 2, Info: 3}, "E:1 W:2 I:3"},
		{DiagCounts{Warnings: 2, Info: 3}, "W:2 I:3"},
		{DiagCounts{Errors: 12, Warnings: 7}, "E:12 W:7"},
	}
	for _, tt := range tests {
		if got := tt.c.Text(); got != tt.want {
			t.Errorf("DiagCounts%+v.Text() = %q, want %q", tt.c, got, tt.want)
		}
		if any := tt.c.Any(); any != (tt.want != "") {
			t.Errorf("DiagCounts%+v.Any() = %v, want %v", tt.c, any, tt.want != "")
		}
	}
}

// TestStatusRowDiagnosticsBadgeSitsNextToThePill checks the badge's place: after the
// LSP pill (and the right plugin items) and before the palette hint, in Fresh's
// bracket style around its E:/W:/I: text.
func TestStatusRowDiagnosticsBadgeSitsNextToThePill(t *testing.T) {
	s := StatusView{
		Position: "Ln 1, Col 1", Lines: 4, EOL: "LF", Encoding: "UTF-8", Language: "Go",
		LSP: "LSP (off)", Diags: DiagCounts{Errors: 1, Warnings: 2},
	}
	runs := StatusRuns(100, s)
	at := func(text string) *Run {
		for i := range runs {
			if runs[i].Text == text {
				return &runs[i]
			}
		}
		return nil
	}
	pill, badge, palette := at(" LSP (off) "), at(" [E:1 W:2] "), at(" "+PaletteHelp+" ")
	switch {
	case pill == nil || pill.Kind != HitStatusSegment || pill.Index != StatusSegmentLSP:
		t.Fatalf("the pill run = %+v, want a clickable LSP segment", pill)
	case badge == nil:
		t.Fatalf("no badge run in %+v", runs)
	case palette == nil:
		t.Fatalf("no palette hint in %+v", runs)
	case pill.X >= badge.X || badge.X >= palette.X:
		t.Errorf("pill at %d, badge at %d, palette at %d: want the badge between the pill and the hint", pill.X, badge.X, palette.X)
	}
	if badge.Kind != HitNone {
		t.Errorf("the badge is clickable (%v); it is a count, not a target", badge.Kind)
	}
}

// TestStatusRowReservesTheDiagnosticsSlotUntilTheCountsAreKnown checks the blank
// slot: while a server runs but has published nothing, the row keeps the cells a
// one-severity badge takes, so the right group does not jump when the first
// diagnostic arrives.
func TestStatusRowReservesTheDiagnosticsSlotUntilTheCountsAreKnown(t *testing.T) {
	base := StatusView{Position: "Ln 1, Col 1", Lines: 4, EOL: "LF", Encoding: "UTF-8", Language: "Go", LSP: "LSP", DiagSlot: true}
	blank := func(runs []Run) *Run {
		for i := range runs {
			if runs[i].Kind == HitNone && runs[i].Text == strings.Repeat(" ", diagBlankWidth) {
				return &runs[i]
			}
		}
		return nil
	}
	slot := blank(StatusRuns(100, base))
	if slot == nil {
		t.Fatalf("no blank diagnostics slot in %+v", StatusRuns(100, base))
	}
	if want := ansi.StringWidth(" [E:1] "); slot.W != want {
		t.Errorf("the slot is %d cells, want the %d a one-severity badge takes", slot.W, want)
	}
	// The counts replace the slot; the two never sit side by side.
	withDiags := base
	withDiags.Diags = DiagCounts{Errors: 1}
	if blank(StatusRuns(100, withDiags)) != nil {
		t.Error("the blank slot is still there after the counts arrived")
	}
	// A document with no server reserves nothing.
	plain := base
	plain.LSP, plain.DiagSlot = "", false
	if blank(StatusRuns(100, plain)) != nil {
		t.Error("a document with no server reserves a diagnostics slot")
	}
}

// TestNarrowStatusRowDropsTheBadgeBeforeThePill checks the right group's order on a
// narrow row: the palette hint goes first, then the diagnostics badge, and the LSP
// pill and the language keep their place. Every run stays inside the row.
func TestNarrowStatusRowDropsTheBadgeBeforeThePill(t *testing.T) {
	s := StatusView{
		Position: "Ln 1, Col 1", Lines: 4, EOL: "LF", Encoding: "UTF-8", Language: "Go",
		LSP: "LSP (off)", Diags: DiagCounts{Errors: 1, Warnings: 2},
	}
	hasText := func(runs []Run, text string) bool {
		for _, r := range runs {
			if r.Text == text {
				return true
			}
		}
		return false
	}
	pill, badge := " LSP (off) ", " [E:1 W:2] "
	both, dropped := 0, false
	for w := 12; w <= 100; w++ {
		runs := StatusRuns(w, s)
		for _, r := range runs {
			if r.X < 0 || r.X+r.W > w {
				t.Fatalf("w=%d: run %+v is outside the row", w, r)
			}
		}
		hasPill, hasBadge := hasText(runs, pill), hasText(runs, badge)
		if hasBadge && !hasPill {
			t.Fatalf("w=%d: the badge is on the row without the pill: %+v", w, runs)
		}
		if hasPill && !hasBadge {
			dropped = true
		}
		if hasPill && hasBadge {
			both = w
		}
	}
	if !dropped {
		t.Error("no width drops the badge while the pill stays; the right group's order changed")
	}
	if both == 0 {
		t.Error("no width shows both the pill and the badge")
	}
}

// TestTrustPillLeadsTheLeftGroup checks Fresh's bottom-left guarantee: the workspace
// trust level is the first element of the status row, it is clickable, and Restricted
// and Blocked take the theme's warning role (Trusted stays on the bar's own style).
func TestTrustPillLeadsTheLeftGroup(t *testing.T) {
	tests := []struct {
		trust string
		warn  bool
		role  RunStyle
	}{
		{"Trusted", false, StyleDefault},
		{"Restricted", true, StyleWarning},
		{"Blocked", true, StyleWarning},
	}
	for _, tt := range tests {
		s := StatusView{Trust: tt.trust, TrustWarn: tt.warn, Position: "Ln 1, Col 1", Lines: 4, EOL: "LF", Encoding: "UTF-8", Language: "Go"}
		runs := StatusRuns(100, s)
		first := runs[0]
		if first.X != 0 || first.Text != " "+tt.trust+" " {
			t.Errorf("%s: the first left run = %+v, want it at column 0", tt.trust, first)
		}
		if first.Kind != HitStatusSegment || first.Index != StatusSegmentTrust {
			t.Errorf("%s: the first run kind = %v index %d, want the trust segment", tt.trust, first.Kind, first.Index)
		}
		if first.Style != tt.role {
			t.Errorf("%s: style = %v, want %v", tt.trust, first.Style, tt.role)
		}
		l := ComputeLayout(100, 30, Input{Status: s})
		if got := l.Hit(1, l.StatusY); got.Kind != HitStatusSegment || got.Index != StatusSegmentTrust {
			t.Errorf("%s: hit at column 1 = %+v, want the trust segment", tt.trust, got)
		}
	}
}

// TestStatusRowWithoutTrustStillLeadsWithLocal checks the row a session without a trust
// port keeps: the plain " Local " label, not clickable.
func TestStatusRowWithoutTrustStillLeadsWithLocal(t *testing.T) {
	s := StatusView{Position: "Ln 1, Col 1", Lines: 4, EOL: "LF"}
	runs := StatusRuns(100, s)
	if len(runs) == 0 || runs[0].X != 0 || runs[0].Text != " Local " || runs[0].Kind != HitNone {
		t.Fatalf("the first left run = %+v, want the plain Local label", runs)
	}
}

// TestNarrowStatusRowKeepsTheTrustPill checks the squeeze order of the left group: the
// trust level is what survives, the cursor position is what is cut. A label that does
// not fit at all is cut with Fresh's ellipsis, never moved off column 0.
func TestNarrowStatusRowKeepsTheTrustPill(t *testing.T) {
	const label = " Restricted "
	s := StatusView{Trust: "Restricted", TrustWarn: true, Position: "Ln 1, Col 1", Lines: 4, EOL: "LF", Encoding: "UTF-8", Language: "Go"}
	for w := 8; w <= 60; w++ {
		runs := StatusRuns(w, s)
		if len(runs) == 0 || runs[0].X != 0 {
			t.Fatalf("w=%d: no run at column 0: %+v", w, runs)
		}
		first := runs[0]
		if first.Kind != HitStatusSegment || first.Index != StatusSegmentTrust {
			t.Fatalf("w=%d: the first run = %+v, want the trust segment", w, first)
		}
		if first.W > w {
			t.Fatalf("w=%d: the trust run is %d cells wide", w, first.W)
		}
		for _, r := range runs {
			if r.X < 0 || r.X+r.W > w {
				t.Fatalf("w=%d: run %+v is outside the row", w, r)
			}
		}
	}
	// Wide enough for the whole pill and the right group: the pill is never padded or
	// shortened, and the position is what gives way.
	first := StatusRuns(100, s)[0]
	if first.Text != label {
		t.Errorf("the trust pill at a wide row = %q, want the whole label", first.Text)
	}
}
