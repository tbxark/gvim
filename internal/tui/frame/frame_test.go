package frame

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/internal/tui/sidebar"
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

// TestStatusRowDiagnosticsBadgeFollowsTheConfiguredList checks that the badge is the
// {diagnostics} element and so follows the configured lists: Fresh's default puts it
// on the left after the cursor position, and a config that names it in the right
// group renders it there, in Fresh's bracket style around its E:/W:/I: text. It is a
// count, not a click target.
func TestStatusRowDiagnosticsBadgeFollowsTheConfiguredList(t *testing.T) {
	base := StatusView{
		Position: "Ln 1, Col 1", Line: 1, Col: 1, Cursors: 1, Lines: 4,
		EOL: "LF", Encoding: "UTF-8", Language: "Go", LSP: "LSP (off)",
		Diags: DiagCounts{Errors: 1, Warnings: 2},
	}
	find := func(runs []Run, text string) *Run {
		for i := range runs {
			if runs[i].Text == text {
				return &runs[i]
			}
		}
		return nil
	}
	badge := " [E:1 W:2] "
	runs := StatusRuns(100, base)
	got := find(runs, badge)
	if got == nil {
		t.Fatalf("no badge run in %+v", runs)
	}
	if got.Kind != HitNone {
		t.Errorf("the badge is clickable (%v); it is a count, not a target", got.Kind)
	}
	var cursor *Run
	for i := range runs {
		if runs[i].Kind == HitStatusSegment && runs[i].Index == StatusSegmentPosition {
			cursor = &runs[i]
		}
	}
	if cursor == nil || cursor.X >= got.X {
		t.Errorf("badge at %d, cursor %+v: Fresh's default list puts the badge after the cursor", got.X, cursor)
	}
	if pill := find(runs, " LSP (off) "); pill == nil || got.X >= pill.X {
		t.Errorf("badge at %d, pill %+v: the badge is a left element and the pill a right one", got.X, pill)
	}

	// A configured right group puts the badge where the user named it.
	placed := base
	placed.Row = StatusRow{
		Left: []StatusElement{{Kind: StatusElementMessages}},
		Right: []StatusElement{
			{Kind: StatusElementLanguage}, {Kind: StatusElementLSP},
			{Kind: StatusElementDiagnostics}, {Kind: StatusElementPalette},
		},
	}
	runs = StatusRuns(100, placed)
	badgeRun := find(runs, badge)
	pill, palette := find(runs, " LSP (off) "), find(runs, " "+PaletteHelp+" ")
	switch {
	case badgeRun == nil:
		t.Fatalf("no configured badge run in %+v", runs)
	case pill == nil || palette == nil:
		t.Fatalf("pill %+v, palette %+v in %+v", pill, palette, runs)
	case pill.X >= badgeRun.X || badgeRun.X >= palette.X:
		t.Errorf("pill at %d, badge at %d, palette at %d: want the configured order", pill.X, badgeRun.X, palette.X)
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

// TestNarrowStatusRowDropsTheRightGroupFromItsEnd checks Fresh's squeeze rule with
// configured lists: a too-wide right group loses its right-most element first, so the
// palette hint goes before the LSP pill and the pill before the language; the left
// group is elided at its tail. Every run stays inside the row.
func TestNarrowStatusRowDropsTheRightGroupFromItsEnd(t *testing.T) {
	s := StatusView{
		Position: "Ln 1, Col 1", Line: 1, Col: 1, Cursors: 1, Lines: 4,
		EOL: "LF", Encoding: "UTF-8", Language: "Go", LSP: "LSP (off)",
		Row: StatusRow{
			Left:  []StatusElement{{Kind: StatusElementTrust}, {Kind: StatusElementMessages}},
			Right: []StatusElement{{Kind: StatusElementLanguage}, {Kind: StatusElementLSP}, {Kind: StatusElementPalette}},
		},
		Trust: "Trusted",
	}
	has := func(runs []Run, text string) bool {
		for _, r := range runs {
			if r.Text == text {
				return true
			}
		}
		return false
	}
	const lang, pill, palette = " Go ", " LSP (off) ", " " + PaletteHelp + " "
	sawPaletteDropped := false
	for w := 8; w <= 100; w++ {
		runs := StatusRuns(w, s)
		for _, r := range runs {
			if r.X < 0 || r.X+r.W > w {
				t.Fatalf("w=%d: run %+v is outside the row", w, r)
			}
		}
		hasLang, hasPill, hasPalette := has(runs, lang), has(runs, pill), has(runs, palette)
		if hasPalette && !hasPill {
			t.Fatalf("w=%d: the palette hint is on the row without the pill: %+v", w, runs)
		}
		if hasPill && !hasLang {
			t.Fatalf("w=%d: the pill is on the row without the language: %+v", w, runs)
		}
		if hasLang && !hasPalette {
			sawPaletteDropped = true
		}
	}
	if !sawPaletteDropped {
		t.Error("no width drops the palette hint while the language stays; the right group's order changed")
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

// TestStatusRowUsesFreshDefaultListsWhenUnset checks the zero StatusView: the row is
// Fresh's default element lists (left trust, remote, terminal_restart, cursor,
// diagnostics, cursor_count, messages; right read_only, line_ending, encoding,
// language, lsp, warnings, update, palette), with the elements gvim does not paint
// simply absent.
func TestStatusRowUsesFreshDefaultListsWhenUnset(t *testing.T) {
	s := StatusView{
		Position: "Ln 1, Col 1", Line: 1, Col: 1, Cursors: 1, Lines: 4,
		EOL: "LF", Encoding: "UTF-8", Language: "Go", LSP: "LSP", Trust: "Trusted",
	}
	left, right := DefaultStatusLeft(), DefaultStatusRight()
	if len(left) != 7 || left[0].Kind != StatusElementTrust || left[6].Kind != StatusElementMessages {
		t.Errorf("default left list = %+v, want Fresh's seven elements from {trust} to {messages}", left)
	}
	if len(right) != 8 || right[0].Kind != StatusElementReadOnly || right[7].Kind != StatusElementPalette {
		t.Errorf("default right list = %+v, want Fresh's eight elements from {read_only} to {palette}", right)
	}
	runs := StatusRuns(100, s)
	if len(runs) == 0 || runs[0].Text != " Trusted " {
		t.Fatalf("first run = %+v, want the trust pill first", runs)
	}
	at := func(text string) int {
		for _, r := range runs {
			if r.Text == text {
				return r.X
			}
		}
		return -1
	}
	// The rows paint in list order: trust, Local, cursor, then the right group's
	// EOL, encoding, language and pill from the right edge.
	if x := at(" Local "); x < 0 {
		t.Errorf("no remote label in %+v", runs)
	}
	if x, y := at(" LF "), at(" UTF-8 "); x < 0 || y < 0 || x >= y {
		t.Errorf("line ending and encoding = %d, %d; want them in that order", x, y)
	}
	if at(" LSP ") < 0 {
		t.Errorf("no LSP pill in %+v", runs)
	}
	if at(" "+PaletteHelp+" ") < 0 {
		t.Errorf("no palette hint in %+v", runs)
	}
}

// TestStatusRowWalksTheConfiguredLists checks that the row is exactly the configured
// elements: order, a missing element gone, and the separator between neighbours.
func TestStatusRowWalksTheConfiguredLists(t *testing.T) {
	s := StatusView{
		Position: "Ln 1, Col 1", Line: 1, Col: 1, Cursors: 1, Lines: 4,
		EOL: "LF", Encoding: "UTF-8", Language: "Go", LSP: "LSP", ReadOnly: true,
		Row: StatusRow{
			Left:      []StatusElement{{Kind: StatusElementCursor}, {Kind: StatusElementMessages}},
			Right:     []StatusElement{{Kind: StatusElementReadOnly}, {Kind: StatusElementLanguage}, {Kind: StatusElementPalette}},
			Separator: "|",
		},
	}
	runs := StatusRuns(120, s)
	var texts []string
	for _, r := range runs {
		texts = append(texts, r.Text)
	}
	joined := strings.Join(texts, "\x00")
	for _, want := range []string{" Ln 1, Col 1   ", "|", " Go ", " [RO] ", " " + PaletteHelp + " "} {
		if !strings.Contains(joined, want) {
			t.Errorf("row %q has no %q", texts, want)
		}
	}
	// No EOL, encoding or LSP: they are not configured.
	for _, gone := range []string{" LF ", " UTF-8 ", " LSP "} {
		if strings.Contains(joined, gone) {
			t.Errorf("row paints %q although the list does not name it: %q", gone, texts)
		}
	}
	// The separator sits between neighbours, never first or last.
	for i, r := range runs {
		if r.Text != "|" {
			continue
		}
		if i == 0 || i == len(runs)-1 {
			t.Errorf("separator at run %d of %d: it has no neighbour on one side", i, len(runs))
		}
		if runs[i-1].X+runs[i-1].W != r.X || r.X+r.W != runs[i+1].X {
			t.Errorf("separator %+v is not between %+v and %+v", r, runs[i-1], runs[i+1])
		}
	}
	// An explicitly empty list paints nothing.
	empty := s
	empty.Row = StatusRow{Left: []StatusElement{}, Right: []StatusElement{}}
	if runs := StatusRuns(80, empty); len(runs) != 0 {
		t.Errorf("an empty configured row paints %+v", runs)
	}
}

// TestStatusRowElementsShowTheirState checks the elements whose text depends on the
// session: {cursor:compact}, {cursor_count}, {terminal_restart} and the clickable
// {messages}, plus the elements gvim parses but does not paint.
func TestStatusRowElementsShowTheirState(t *testing.T) {
	row := func(kinds ...StatusElementKind) StatusRow {
		els := make([]StatusElement, len(kinds))
		for i, k := range kinds {
			els[i] = StatusElement{Kind: k}
		}
		return StatusRow{Left: els}
	}
	base := StatusView{
		Position: "Ln 7, Col 3", Line: 7, Col: 3, Cursors: 1, Lines: 100,
		EOL: "LF", Encoding: "UTF-8", Language: "Go",
	}
	text := func(s StatusView) string {
		var b strings.Builder
		for _, r := range StatusRuns(120, s) {
			b.WriteString(r.Text)
		}
		return b.String()
	}
	// {cursor:compact} pads "7:3" to the compact width (line digits + colon + reserve).
	compact := base
	compact.Row = row(StatusElementCursorCompact)
	if want := " 7:3     "; text(compact) != want {
		t.Errorf("compact cursor = %q, want %q", text(compact), want)
	}
	// {cursor_count} shows above one cursor only.
	count := base
	count.Row = row(StatusElementCursorCount)
	if got := text(count); got != "" {
		t.Errorf("one cursor paints %q, want nothing", got)
	}
	count.Cursors = 3
	if got := text(count); got != " 3 cursors " {
		t.Errorf("three cursors = %q, want %q", got, " 3 cursors ")
	}
	// {terminal_restart} renders only while the dock's shell has exited, and clicks
	// restart it.
	restart := base
	restart.Row = row(StatusElementTerminalRestart)
	if got := text(restart); got != "" {
		t.Errorf("a running dock paints %q, want nothing", got)
	}
	restart.TerminalRestart = "Restart terminal"
	runs := StatusRuns(120, restart)
	if len(runs) != 1 || runs[0].Text != " Restart terminal " ||
		runs[0].Kind != HitStatusSegment || runs[0].Index != StatusSegmentTerminalRestart {
		t.Errorf("terminal restart run = %+v, want a clickable restart segment", runs)
	}
	// {messages} is clickable: it opens the warnings list.
	msg := base
	msg.Row = row(StatusElementMessages)
	msg.Message = "Saved"
	runs = StatusRuns(120, msg)
	if len(runs) != 1 || runs[0].Kind != HitStatusSegment || runs[0].Index != StatusSegmentMessages {
		t.Errorf("messages run = %+v, want a clickable message segment", runs)
	}
	// The elements gvim parses but does not paint render nothing: {warnings} and
	// plugin tokens. ({filename}, {chord}, {clock} and {update} paint now; see
	// TestStatusRowResidualElements and TestStatusUpdateElement.)
	hidden := base
	hidden.Row = row(StatusElementWarnings, StatusElementCustom)
	hidden.Row.Left[1].Key = "plugin:token"
	if got := text(hidden); got != "" {
		t.Errorf("hidden elements paint %q, want nothing", got)
	}
}

// TestStatusRowPluginItemsSitAtTheirAnchors checks the gvim plugin items: the left
// ones between the position and the message, the right ones before the trailing
// counter elements, clickable items keeping their hit target.
func TestStatusRowPluginItemsSitAtTheirAnchors(t *testing.T) {
	s := StatusView{
		Position: "Ln 1, Col 1", Line: 1, Col: 1, Cursors: 1, Lines: 4,
		EOL: "LF", Encoding: "UTF-8", Language: "Go", LSP: "LSP",
		Message: "Saved",
		Extra: []StatusItem{
			{ID: "mode", Plugin: "vi", Text: "-- NORMAL --"},
			{ID: "branch", Plugin: "git", Text: "main", Right: true, Clickable: true},
		},
	}
	runs := StatusRuns(120, s)
	at := func(text string) *Run {
		for i := range runs {
			if runs[i].Text == text {
				return &runs[i]
			}
		}
		return nil
	}
	var cursor *Run
	for i := range runs {
		if runs[i].Kind == HitStatusSegment && runs[i].Index == StatusSegmentPosition {
			cursor = &runs[i]
		}
	}
	mode, msg := at(" -- NORMAL -- "), at(" Saved ")
	if cursor == nil || mode == nil || msg == nil {
		t.Fatalf("cursor %+v, left item %+v, message %+v in %+v", cursor, mode, msg, runs)
	}
	if cursor.X >= mode.X || mode.X >= msg.X {
		t.Errorf("cursor %d, left plugin item %d, message %d: want the item between position and message", cursor.X, mode.X, msg.X)
	}
	branch, pill, palette := at(" main "), at(" LSP "), at(" "+PaletteHelp+" ")
	if branch == nil || pill == nil || palette == nil {
		t.Fatalf("right item %+v, pill %+v, palette %+v in %+v", branch, pill, palette, runs)
	}
	if pill.X >= branch.X || branch.X >= palette.X {
		t.Errorf("pill %d, right plugin item %d, palette %d: want the item after the language cluster", pill.X, branch.X, palette.X)
	}
	if branch.Kind != HitStatusPlugin || branch.Index != 1 {
		t.Errorf("right plugin item = %+v, want a clickable hit target for extra 1", branch)
	}
}

// TestParseStatusElements checks the config names: braces are optional, unknown
// names are rejected, a colon is a plugin token, and nil and empty differ.
func TestParseStatusElements(t *testing.T) {
	tests := []struct {
		name string
		kind StatusElementKind
		ok   bool
	}{
		{"{cursor}", StatusElementCursor, true},
		{"cursor", StatusElementCursor, true},
		{" {cursor:compact} ", StatusElementCursorCompact, true},
		{"{terminal_restart}", StatusElementTerminalRestart, true},
		{"{read_only}", StatusElementReadOnly, true},
		{"{remote}", StatusElementRemote, true},
		{"{trust}", StatusElementTrust, true},
		{"{plugin:token}", StatusElementCustom, true},
		{"{no_such_element}", StatusElementFilename, false},
	}
	for _, tt := range tests {
		got, ok := ParseStatusElement(tt.name)
		if ok != tt.ok {
			t.Errorf("ParseStatusElement(%q) ok = %v, want %v", tt.name, ok, tt.ok)
			continue
		}
		if ok && got.Kind != tt.kind {
			t.Errorf("ParseStatusElement(%q) = %+v, want kind %v", tt.name, got, tt.kind)
		}
	}
	if got := ParseStatusElements(nil); got != nil {
		t.Errorf("nil list parses to %+v, want nil", got)
	}
	if got := ParseStatusElements([]string{}); got == nil || len(got) != 0 {
		t.Errorf("empty list parses to %+v, want an empty non-nil side", got)
	}
	if got := ParseStatusElements([]string{"{trust}", "{no_such_element}", "{language}"}); len(got) != 2 {
		t.Errorf("unknown names are kept: %+v", got)
	}
}

// TestNarrowStatusRowDropsTheRemoteLabelWithOneSeparator checks the label the squeeze
// rule gives up first: when the "Local" ({remote}) element is dropped, the configured
// separator between its neighbours is not left doubled.
func TestNarrowStatusRowDropsTheRemoteLabelWithOneSeparator(t *testing.T) {
	s := StatusView{
		Position: "Ln 1, Col 1", Line: 1, Col: 1, Cursors: 1, Lines: 4,
		EOL: "LF", Encoding: "UTF-8", Language: "Go", Trust: "Trusted", Message: "Saved",
		Row: StatusRow{
			Left:      []StatusElement{{Kind: StatusElementTrust}, {Kind: StatusElementRemote}, {Kind: StatusElementMessages}},
			Separator: "|",
		},
		Extra: []StatusItem{{ID: "m", Plugin: "p", Text: "xx"}},
	}
	dropped := false
	for w := 12; w <= 60; w++ {
		runs := StatusRuns(w, s)
		hasRemote := false
		for i, r := range runs {
			if r.Text == " Local " {
				hasRemote = true
			}
			if r.Text == "|" && i > 0 && runs[i-1].Text == "|" {
				t.Errorf("w=%d: two separators are adjacent: %+v", w, runs)
			}
		}
		if !hasRemote {
			dropped = true
		}
	}
	if !dropped {
		t.Error("no width drops the Local label with a plugin item on the row")
	}
}

// TestExplorerWidthForms checks file_explorer.width in the layout: a percentage
// of the frame, a column count, the automatic width for the zero value, and the
// gvim clamps (min 16, max 60, the editor keeps 20 columns on a narrow frame).
func TestExplorerWidthForms(t *testing.T) {
	tests := []struct {
		name  string
		w     int
		width explorer.Width
		want  int
	}{
		{"automatic is gvim's 32", 100, explorer.Width{}, 32},
		{"percent of the frame", 100, explorer.PercentWidth(30), 30},
		{"percent rounds down", 101, explorer.PercentWidth(30), 30},
		{"percent below the floor clamps to 16", 40, explorer.PercentWidth(30), 16},
		{"percent above the ceiling clamps to 60", 140, explorer.PercentWidth(60), 60},
		{"columns", 100, explorer.ColumnsWidth(24), 24},
		{"columns below the floor clamp", 100, explorer.ColumnsWidth(2), 16},
		{"columns above the ceiling clamp", 100, explorer.ColumnsWidth(200), 60},
		{"the editor keeps 20 columns", 40, explorer.ColumnsWidth(200), 20},
		{"a frame narrower than the floor", 10, explorer.ColumnsWidth(8), 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExplorerCols(tt.w, tt.width); got != tt.want {
				t.Errorf("ExplorerCols(%d, %+v) = %d, want %d", tt.w, tt.width, got, tt.want)
			}
		})
	}
}

// TestRightSideExplorerGeometry checks file_explorer.side right: the column takes
// the right edge, the editor area starts at column 0 and ends at the column, the
// tab strip and its "+" are pinned inside the editor area, and the hit map reads
// the same columns.
func TestRightSideExplorerGeometry(t *testing.T) {
	const w = 100
	in := Input{
		ExplorerOpen:  true,
		ExplorerWidth: explorer.ColumnsWidth(30),
		ExplorerSide:  explorer.SideRight,
		Tabs:          stripTabs(),
	}
	l := ComputeLayout(w, 30, in)
	if !l.ExplorerOpen || !l.ExRight || l.Ex != 30 || l.ExX != w-30 {
		t.Fatalf("ex %d at %d right %v, want 30 at %d", l.Ex, l.ExX, l.ExRight, w-30)
	}
	if l.EditorX != 0 || l.EditorW != w-30 {
		t.Fatalf("editor %d..%d, want 0..%d", l.EditorX, l.EditorX+l.EditorW, w-30)
	}
	// The tab strip begins at the frame's left edge and its "+" ends at the
	// editor's right edge.
	if len(l.Strip.Runs) == 0 {
		t.Fatal("no tab strip runs")
	}
	if first := l.Strip.Runs[0]; first.X != 0 {
		t.Errorf("first strip run at %d, want column 0", first.X)
	}
	last := l.Strip.Runs[len(l.Strip.Runs)-1]
	if last.Kind != HitTabNew || last.X+last.W != l.EditorX+l.EditorW {
		t.Errorf("plus run %+v, want it pinned to the editor's right edge %d", last, l.EditorX+l.EditorW)
	}
	// The explorer wins the cells past its first column; the editor keeps the rest.
	if got := l.Hit(l.ExX+3, l.ExBodyTop); got.Kind != HitExplorerBody && got.Kind != HitExplorerRow {
		t.Errorf("hit in the right explorer column = %+v", got)
	}
	if got := l.Hit(l.TextX+1, l.BodyTop+1); got.Kind != HitEditorText {
		t.Errorf("hit in the editor column = %+v", got)
	}
	if got := l.Hit(l.ExX, l.BodyTop+1); got.Kind != HitExplorerGrip {
		t.Errorf("hit on the editor-facing border = %+v, want the grip", got)
	}
	// The explorer's buttons are frame columns on this side.
	if l.ExReloadX < l.ExX {
		t.Errorf("reload button at %d, want it in the right column from %d", l.ExReloadX, l.ExX)
	}
	if got := l.Hit(l.ExReloadX, l.ExTitleY); got.Kind != HitExplorerTitleReload {
		t.Errorf("hit on the reload button = %+v", got)
	}
}

// TestRightSideExplorerSections checks the section stack on the right side: the
// headers and bodies are in the right column, the bottom border closes it, and
// the hit map's section bodies keep the panel's orientation.
func TestRightSideExplorerSections(t *testing.T) {
	const w = 90
	in := Input{
		ExplorerOpen:  true,
		ExplorerWidth: explorer.ColumnsWidth(24),
		ExplorerSide:  explorer.SideRight,
		Sidebar:       []sidebar.Spec{{Kind: sidebar.KindExplorer}, {Kind: sidebar.KindContents}},
		ExplorerRows:  5,
	}
	l := ComputeLayout(w, 24, in)
	if len(l.Sections) != 2 || l.ExX != w-24 {
		t.Fatalf("sections %d at %d, want two in the right column from %d", len(l.Sections), l.ExX, w-24)
	}
	// The Contents close button is an absolute frame column too.
	sec := l.Sections[1]
	if got := l.Hit(l.SectionClose[1], sec.HeaderY); got.Kind != HitSidebarClose || got.Section != 1 {
		t.Errorf("hit on the Contents close button = %+v", got)
	}
	if got := l.Hit(l.ExX+1, sec.BodyTop); got.Kind != HitSidebarRow || got.Section != 1 {
		t.Errorf("hit on the section's first row = %+v", got)
	}
}

// TestRightSideNoticeBandStartsAtTheEditorEdge checks the notice band on the right
// side: it spans the editor area (columns 0 to EditorW-1), never the explorer
// column, and the status row keeps its full width.
func TestRightSideNoticeBandStartsAtTheEditorEdge(t *testing.T) {
	long := strings.Repeat("a long notice that cannot fit the row ", 6)
	in := Input{
		ExplorerOpen:  true,
		ExplorerWidth: explorer.ColumnsWidth(30),
		ExplorerSide:  explorer.SideRight,
		Status:        StatusView{Position: "Ln 1, Col 1", Lines: 4, EOL: "LF", Message: long},
	}
	l := ComputeLayout(100, 20, in)
	if len(l.Notice) == 0 {
		t.Fatal("no notice band on a narrow row")
	}
	if l.NoticeX != 0 {
		t.Errorf("notice band starts at %d, want the editor's left edge 0", l.NoticeX)
	}
	if l.NoticeX+l.EditorW > l.ExX {
		t.Errorf("notice band ends at %d, past the explorer column at %d", l.NoticeX+l.EditorW, l.ExX)
	}
	// The status row still spans the frame.
	var last Run
	if n := len(l.Status); n > 0 {
		last = l.Status[n-1]
	}
	if last.X+last.W != 100 {
		t.Errorf("status row ends at %d, want the frame edge 100", last.X+last.W)
	}
}

// TestStatusRowResidualElements checks the three elements round 2 parsed but did not
// paint: {filename} with Fresh's modified and read-only markers, the muted {chord}
// of a pending sequence, and the {clock} time.
func TestStatusRowResidualElements(t *testing.T) {
	one := func(k StatusElementKind) StatusRow {
		return StatusRow{Left: []StatusElement{{Kind: k}}}
	}
	tests := []struct {
		name  string
		row   StatusRow
		state StatusView
		want  string
		style RunStyle
	}{
		{"filename", one(StatusElementFilename), StatusView{Filename: "docs/ui.md"}, " docs/ui.md ", StyleDefault},
		{"modified filename", one(StatusElementFilename), StatusView{Filename: "docs/ui.md", FilenameModified: true}, " docs/ui.md [+] ", StyleDefault},
		{"read-only filename", one(StatusElementFilename), StatusView{Filename: "docs/ui.md", ReadOnly: true}, " docs/ui.md [RO] ", StyleDefault},
		{"modified read-only filename", one(StatusElementFilename), StatusView{Filename: "docs/ui.md", FilenameModified: true, ReadOnly: true}, " docs/ui.md [+] [RO] ", StyleDefault},
		{"untitled filename", one(StatusElementFilename), StatusView{Filename: "[No Name]"}, " [No Name] ", StyleDefault},
		{"no filename paints nothing", one(StatusElementFilename), StatusView{}, "", StyleDefault},
		{"pending chord", one(StatusElementChord), StatusView{Chord: "[Ctrl+X]"}, " [Ctrl+X] ", StyleMuted},
		{"no pending chord paints nothing", one(StatusElementChord), StatusView{}, "", StyleDefault},
		{"clock", one(StatusElementClock), StatusView{Clock: "09:05"}, " 09:05 ", StyleDefault},
		{"no clock paints nothing", one(StatusElementClock), StatusView{}, "", StyleDefault},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.state
			s.Row = tt.row
			runs := StatusRuns(120, s)
			var got strings.Builder
			for _, r := range runs {
				got.WriteString(r.Text)
			}
			if got.String() != tt.want {
				t.Fatalf("row = %q, want %q", got.String(), tt.want)
			}
			if tt.want != "" && runs[0].Style != tt.style {
				t.Errorf("run style = %v, want %v", runs[0].Style, tt.style)
			}
		})
	}
	// In a configured list the three keep their order and their styles.
	s := StatusView{
		Filename: "src/main.go", FilenameModified: true, Chord: "[Ctrl+X]", Clock: "23:59",
		Row: StatusRow{Left: []StatusElement{
			{Kind: StatusElementFilename}, {Kind: StatusElementChord}, {Kind: StatusElementClock},
		}},
	}
	runs := StatusRuns(120, s)
	wantTexts := []string{" src/main.go [+] ", " [Ctrl+X] ", " 23:59 "}
	if len(runs) != len(wantTexts) {
		t.Fatalf("runs = %+v, want %d", runs, len(wantTexts))
	}
	for i, want := range wantTexts {
		if runs[i].Text != want {
			t.Errorf("run %d = %q, want %q", i, runs[i].Text, want)
		}
	}
	if runs[1].Style != StyleMuted {
		t.Errorf("the chord run style = %v, want muted", runs[1].Style)
	}
}

// TestStatusRowResidualElementsElideAtTheTail checks the narrow-width behaviour: they
// are ordinary left elements, so the left group's tail elides and drops as the row
// narrows, and widening paints them again.
func TestStatusRowResidualElementsElideAtTheTail(t *testing.T) {
	s := StatusView{
		Filename: "src/internal/tui/status_elements.go", // 38 cells with padding
		Chord:    "[Ctrl+X]",
		Clock:    "23:59",
		Row: StatusRow{Left: []StatusElement{
			{Kind: StatusElementFilename}, {Kind: StatusElementChord}, {Kind: StatusElementClock},
		}},
	}
	text := func(w int) string {
		var b strings.Builder
		for _, r := range StatusRuns(w, s) {
			b.WriteString(r.Text)
		}
		return b.String()
	}
	narrow := text(28)
	if !strings.Contains(narrow, "…") {
		t.Errorf("narrow row %q does not elide the filename", narrow)
	}
	for _, gone := range []string{"[Ctrl+X]", "23:59"} {
		if strings.Contains(narrow, gone) {
			t.Errorf("narrow row %q paints %q past the cut", narrow, gone)
		}
	}
	wide := text(80)
	for _, want := range []string{"status_elements.go", "[Ctrl+X]", "23:59"} {
		if !strings.Contains(wide, want) {
			t.Errorf("wide row %q has no %q", wide, want)
		}
	}
}
