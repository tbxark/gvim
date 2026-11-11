package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/internal/tui/tabstrip"
)

func TestStripLabelMarksModifiedAndPreview(t *testing.T) {
	cases := []struct {
		name string
		tab  TabInfo
		want string
	}{
		{"plain", TabInfo{Name: "a.go"}, "a.go"},
		{"modified marker goes before the name", TabInfo{Name: "a.go", Modified: true}, "● a.go"},
		{"preview from the flag", TabInfo{Name: "b.go", Preview: true}, "b.go (preview)"},
		{"preview label from the editor", TabInfo{Name: "b.go (preview)"}, "b.go (preview)"},
		{"modified preview", TabInfo{Name: "b.go (preview)", Modified: true, Preview: true}, "● b.go (preview)"},
		{"unnamed", TabInfo{}, noNameLabel},
		{"unnamed and modified", TabInfo{Modified: true}, "● " + noNameLabel},
	}
	for _, c := range cases {
		if got := frame.StripLabel(c.tab); got != c.want {
			t.Errorf("%s: frame.StripLabel = %q, want %q", c.name, got, c.want)
		}
	}
}

// The fixture at 100 columns: "  a.txt × " then "  b.txt × " with no gap, and the
// three-cell " + " button pinned to the last columns.
func TestPlaceStripFixtureAt100Columns(t *testing.T) {
	tabs := []TabInfo{{Name: "a.txt", Active: true}, {Name: "b.txt"}}
	s := frame.PlaceStrip(0, 100, tabs, 0, icons.Plain)
	if s.Overflow || s.Before || s.After {
		t.Fatalf("two short tabs overflow a 100-cell row: %+v", s)
	}
	type want struct {
		x    int
		text string
		kind hitKind
		idx  int
	}
	expect := []want{
		{0, "  a.txt ", hitTab, 0},
		{8, "×", hitTabClose, 0},
		{9, " ", hitTab, 0},
		{10, "  b.txt ", hitTab, 1},
		{18, "×", hitTabClose, 1},
		{19, " ", hitTab, 1},
		{97, " + ", hitTabNew, 0},
	}
	if len(s.Runs) != len(expect) {
		t.Fatalf("runs = %+v", s.Runs)
	}
	for i, e := range expect {
		r := s.Runs[i]
		if r.X != e.x || r.Text != e.text || r.Kind != e.kind || r.Index != e.idx {
			t.Errorf("run %d = %+v, want %+v", i, r, e)
		}
	}
}

func TestStripHitRegions(t *testing.T) {
	s := frame.PlaceStrip(0, 100, []TabInfo{{Name: "a.txt"}, {Name: "b.txt"}}, 0, icons.Plain)
	cases := []struct {
		x    int
		want hitTarget
	}{
		{1, hitTarget{Kind: hitTab, Index: 0}},
		{7, hitTarget{Kind: hitTab, Index: 0}},
		{8, hitTarget{Kind: hitTabClose, Index: 0}},
		{9, hitTarget{Kind: hitTab, Index: 0}},
		{10, hitTarget{Kind: hitTab, Index: 1}},
		{18, hitTarget{Kind: hitTabClose, Index: 1}},
		{20, hitTarget{Kind: hitNone}},
		{96, hitTarget{Kind: hitNone}},
		{97, hitTarget{Kind: hitTabNew}},
		{98, hitTarget{Kind: hitTabNew}},
		{99, hitTarget{Kind: hitTabNew}},
	}
	for _, c := range cases {
		if got := frame.StripHit(s.Runs, c.x); got != c.want {
			t.Errorf("frame.StripHit(%d) = %+v, want %+v", c.x, got, c.want)
		}
	}
}

// A modified tab's close glyph is still the × cell, and the marker is not a target.
func TestStripHitWithModifiedTab(t *testing.T) {
	s := frame.PlaceStrip(0, 100, []TabInfo{{Name: "a.txt", Modified: true}}, 0, icons.Plain)
	// "  ● a.txt " is 10 cells, so the × is at column 10.
	if got := frame.StripHit(s.Runs, 10); got != (hitTarget{Kind: hitTabClose, Index: 0}) {
		t.Errorf("close of a modified tab = %+v", got)
	}
	if got := frame.StripHit(s.Runs, 2); got != (hitTarget{Kind: hitTab, Index: 0}) {
		t.Errorf("marker cell = %+v, want the tab name", got)
	}
}

func TestPlaceStripStartsAfterTheExplorerColumns(t *testing.T) {
	s := frame.PlaceStrip(30, 100, []TabInfo{{Name: "a.txt"}}, 0, icons.Plain)
	if s.Runs[0].X != 30 || s.Runs[0].Text != "  a.txt " {
		t.Fatalf("first run = %+v, want the tab at column 30", s.Runs[0])
	}
	if plus := s.Runs[len(s.Runs)-1]; plus.Kind != hitTabNew || plus.X != 97 || plus.W != 3 {
		t.Fatalf("plus = %+v, want the three-cell button at columns 97..99", plus)
	}
}

// Four tabs in a 20-column row do not fit. The active tab stays on screen, and
// the markers show the tabs that are scrolled off.
func TestPlaceStripOverflowKeepsTheActiveTabVisible(t *testing.T) {
	names := []string{"one.go", "two.go", "three.go", "four.go"}
	for active := range names {
		tabs := make([]TabInfo, len(names))
		for i, n := range names {
			tabs[i] = TabInfo{Name: n, Active: i == active}
		}
		first := frame.RevealedFirst(tabs, 0, 20, 0, active)
		s := frame.PlaceStrip(0, 20, tabs, first, icons.Plain)
		if !s.Overflow {
			t.Fatalf("active %d: four tabs do not overflow at 20 columns", active)
		}
		for _, r := range s.Runs {
			if r.Kind == hitTabNew || r.Kind == hitTabScrollBefore || r.Kind == hitTabScrollAfter {
				continue
			}
			if r.X < 1 || r.X+r.W > 16 {
				t.Errorf("active %d: run %+v is outside the window", active, r)
			}
		}
		var activeRuns []run
		for _, r := range s.Runs {
			if r.Kind != hitTabNew && r.Index == active && r.Kind != hitTabScrollBefore && r.Kind != hitTabScrollAfter {
				activeRuns = append(activeRuns, r)
			}
		}
		if len(activeRuns) != 3 {
			t.Fatalf("active %d: %d runs of the active tab are visible, want all 3: %+v", active, len(activeRuns), s.Runs)
		}
		if active == 0 && (s.Before || !s.After) {
			t.Errorf("first tab: before=%v after=%v, want the right marker only", s.Before, s.After)
		}
		if active == len(names)-1 && !s.Before {
			t.Errorf("last tab: before=%v after=%v, want the left marker", s.Before, s.After)
		}
	}
}

// A tab wider than the window shows its start, so its name is not lost.
func TestPlaceStripWideActiveTabShowsItsStart(t *testing.T) {
	long := strings.Repeat("n", 40) + ".go"
	tabs := []TabInfo{{Name: "a.go"}, {Name: long, Active: true}}
	first := frame.RevealedFirst(tabs, 0, 20, 0, 1)
	s := frame.PlaceStrip(0, 20, tabs, first, icons.Plain)
	var start run
	for _, r := range s.Runs {
		if r.Kind == hitTab && r.Index == 1 {
			start = r
			break
		}
	}
	if start.X != 1 || !strings.HasPrefix(start.Text, " ") {
		t.Fatalf("active tab first run = %+v, want its start at the window edge", start)
	}
}

func TestPlaceStripEmptyStripHasOnlyPlus(t *testing.T) {
	s := frame.PlaceStrip(0, 40, nil, 0, icons.Plain)
	if s.Overflow || len(s.Runs) != 1 || s.Runs[0].Kind != hitTabNew || s.Runs[0].X != 37 {
		t.Fatalf("empty strip = %+v", s)
	}
}

func TestDrawTabStripPaintsTheFixtureRow(t *testing.T) {
	g := grid.New(40, 3)
	tabstrip.Draw(g, style.Styles{}, 0, 1, frame.PlaceStrip(0, 40, []TabInfo{{Name: "a.txt", Active: true}}, 0, icons.Plain), frame.HitTarget{})
	if g.At(0, 1).Text != " " || g.At(1, 1).Text != " " || g.At(2, 1).Text != "a" || g.At(8, 1).Text != "×" {
		t.Fatalf("tab row = %q, want the tab at columns 0..9", g.Plain()[1])
	}
	if g.At(37, 1).Text != " " || g.At(38, 1).Text != "+" || g.At(39, 1).Text != " " {
		t.Fatalf("+ button = %q %q %q at columns 37..39, want \" + \"", g.At(37, 1).Text, g.At(38, 1).Text, g.At(39, 1).Text)
	}
}

func TestDrawTabStripMarksTheActiveTab(t *testing.T) {
	st := style.Styles{
		Tab:       lipgloss.NewStyle().Italic(true),
		TabActive: lipgloss.NewStyle().Bold(true),
	}
	g := grid.New(40, 3)
	tabstrip.Draw(g, st, 0, 1, frame.PlaceStrip(0, 40, []TabInfo{{Name: "a.txt"}, {Name: "b.txt", Active: true}}, 0, icons.Plain), frame.HitTarget{})
	if g.At(2, 1).Style.Bold || !g.At(2, 1).Style.Italic {
		t.Fatalf("inactive tab cell style = %+v", g.At(2, 1).Style)
	}
	if !g.At(12, 1).Style.Bold {
		t.Fatalf("active tab cell style = %+v", g.At(12, 1).Style)
	}
	if g.At(18, 1).Text != "×" || !g.At(18, 1).Style.Bold {
		t.Fatalf("active tab close = %q, want the bold ×", g.At(18, 1).Text)
	}
	if got := g.At(9, 1).Text; got != " " {
		t.Errorf("space after close = %q, want a blank", got)
	}
	if got := g.At(10, 1).Text; got != "│" {
		t.Errorf("separator between tabs at column 10 = %q, want '│'", got)
	}
	if g.At(38, 1).Style.Bold || g.At(37, 1).Style.Bold || g.At(39, 1).Style.Bold {
		t.Fatal("the plus button takes the active style")
	}
}

func TestDrawTabStripShowsTheModifiedMarker(t *testing.T) {
	g := grid.New(40, 3)
	tabstrip.Draw(g, style.Styles{}, 0, 1, frame.PlaceStrip(0, 40, []TabInfo{{Name: "a.txt", Modified: true, Active: true}}, 0, icons.Plain), frame.HitTarget{})
	row := strings.TrimRight(g.Plain()[1], " ")
	if !strings.HasPrefix(row, "  ● a.txt × ") {
		t.Fatalf("modified tab row = %q, want the dot before the name", row)
	}
	if strings.Contains(row, "*") {
		t.Fatalf("the legacy star is still drawn: %q", row)
	}
}

func TestDrawTabStripDrawsOverflowMarkers(t *testing.T) {
	tabs := []TabInfo{{Name: "one.go"}, {Name: "two.go"}, {Name: "three.go"}, {Name: "four.go", Active: true}}
	g := grid.New(20, 3)
	tabstrip.Draw(g, style.Styles{}, 0, 1, frame.PlaceStrip(0, 20, tabs, frame.RevealedFirst(tabs, 0, 20, 0, 3), icons.Plain), frame.HitTarget{})
	row := g.Plain()[1]
	if ansi.StringWidth(row) != 20 {
		t.Fatalf("row is %d cells wide", ansi.StringWidth(row))
	}
	if g.At(0, 1).Text != icons.Plain.Glyph(icons.ScrollLeft) {
		t.Fatalf("left marker = %q in %q", g.At(0, 1).Text, row)
	}
	// The last tab is active, so nothing is scrolled off on the right: the marker
	// cell is blank and the three-cell plus button ends the row with the glyph in
	// its middle cell.
	if g.At(16, 1).Text != " " || g.At(17, 1).Text != " " || g.At(18, 1).Text != icons.Plain.Glyph(icons.New) || g.At(19, 1).Text != " " {
		t.Fatalf("right marker and plus = %q %q %q %q in %q", g.At(16, 1).Text, g.At(17, 1).Text, g.At(18, 1).Text, g.At(19, 1).Text, row)
	}
	if !strings.Contains(row, " four.go ×") {
		t.Fatalf("active tab not visible in %q", row)
	}
}

// TestTabStripHoverAndFocus checks the strip's pointer and keyboard feedback: the
// run under the pointer underlines (label, close glyph and the plus).
func TestTabStripHoverAndFocus(t *testing.T) {
	tabs := []TabInfo{{Name: "a.txt", Active: true}, {Name: "b.txt"}}
	s := frame.PlaceStrip(0, 40, tabs, 0, icons.Plain)
	st := style.Styles{Accent: lipgloss.NewStyle().Background(lipgloss.Color("4"))}

	var second, close frame.Run
	for _, r := range s.Runs {
		if r.Kind == frame.HitTab && r.Index == 1 {
			second = r
		}
		if r.Kind == frame.HitTabClose && r.Index == 0 {
			close = r
		}
	}
	if second.W == 0 || close.W == 0 {
		t.Fatalf("strip runs = %+v, want the second tab and the first close", s.Runs)
	}

	hovered := grid.New(40, 2)
	tabstrip.Draw(hovered, st, 0, 1, s, frame.HitTarget{Kind: frame.HitTab, Index: 1})
	for x := second.X; x < second.X+second.W; x++ {
		if !hovered.At(x, 1).Style.Underline {
			t.Fatalf("hovered second tab cell %d is not underlined", x)
		}
	}
	if hovered.At(0, 1).Style.Underline {
		t.Fatal("the first tab underlined without the pointer")
	}
	closed := grid.New(40, 2)
	tabstrip.Draw(closed, st, 0, 1, s, frame.HitTarget{Kind: frame.HitTabClose, Index: 0})
	if !closed.At(close.X, 1).Style.Underline {
		t.Fatalf("the hovered close glyph at %d is not underlined", close.X)
	}

	focused := grid.New(40, 2)
	tabstrip.Draw(focused, st, 0, 1, s, frame.HitTarget{})
	if got := focused.At(0, 1).Text; got != " " {
		t.Fatalf("focused strip leading cell = %q, want the tab padding", got)
	}
	unfocused := grid.New(40, 2)
	tabstrip.Draw(unfocused, st, 0, 1, s, frame.HitTarget{})
	if got := unfocused.At(0, 1).Text; got != " " {
		t.Fatalf("unfocused strip leading cell = %q, want the tab's blank", got)
	}
	// The active tab carries weight; the other one stays regular.
	if !unfocused.At(1, 1).Style.Bold {
		t.Fatal("the active tab label is not bold")
	}
	if unfocused.At(second.X+1, 1).Style.Bold {
		t.Fatal("an inactive tab label is bold")
	}
}

func TestDrawTabStripDrawsTheRightMarkerWhenTabsFollow(t *testing.T) {
	tabs := []TabInfo{{Name: "one.go", Active: true}, {Name: "two.go"}, {Name: "three.go"}, {Name: "four.go"}}
	g := grid.New(20, 3)
	tabstrip.Draw(g, style.Styles{}, 0, 1, frame.PlaceStrip(0, 20, tabs, 0, icons.Plain), frame.HitTarget{})
	row := g.Plain()[1]
	if g.At(0, 1).Text != " " || g.At(16, 1).Text != icons.Plain.Glyph(icons.ScrollRight) {
		t.Fatalf("markers in %q, want only the right marker", row)
	}
}

func TestDrawTabStripWithNoTabsDoesNotPanic(t *testing.T) {
	g := grid.New(10, 3)
	s := frame.PlaceStrip(0, 10, nil, 0, icons.Plain)
	tabstrip.Draw(g, style.Styles{}, 0, 1, s, frame.HitTarget{})
	if len(s.Runs) != 1 || s.Runs[0].Kind != hitTabNew {
		t.Fatalf("runs = %+v", s.Runs)
	}
}

func TestPlaceStripWithNarrowRowDoesNotPanic(t *testing.T) {
	tabs := []TabInfo{{Name: "a.go", Active: true}}
	for w := 0; w <= 6; w++ {
		g := grid.New(w, 2)
		tabstrip.Draw(g, style.Styles{}, 0, 1, frame.PlaceStrip(0, w, tabs, 0, icons.Plain), frame.HitTarget{})
	}
}

// Markers are drawn only when the tabs overflow the row.
func TestTabMarkersAppearOnlyWhenOverflowing(t *testing.T) {
	tabs := []TabInfo{{Name: "a.go"}, {Name: "b.go"}}
	if s := frame.PlaceStrip(0, 40, tabs, 0, icons.Plain); s.Overflow || hasMarker(s) {
		t.Fatalf("two tabs at 40 columns show markers: %+v", s)
	}
	if s := frame.PlaceStrip(0, 12, tabs, 0, icons.Plain); !s.Overflow || !hasMarker(s) {
		t.Fatalf("two tabs at 12 columns show no marker: %+v", s)
	}
}

func hasMarker(s strip) bool {
	for _, r := range s.Runs {
		if (r.Kind == hitTabScrollBefore || r.Kind == hitTabScrollAfter) && r.Text != " " {
			return true
		}
	}
	return false
}

// Scrolling is a clamped tab index. Each step moves one tab, and the window
// never shows blank cells past the last tab.
func TestClampFirstStopsWhenTheTailFillsTheWindow(t *testing.T) {
	tabs := make([]TabInfo, 12)
	for i := range tabs {
		tabs[i] = TabInfo{Name: "t.go"}
	}
	starts, total := frame.TabStarts(tabs)
	window := 60 - 3 - 2 // width 60, the three-cell plus and two markers
	last := frame.ClampFirst(starts, total, window, 99)
	if last >= len(tabs) || total-starts[last] > window {
		t.Fatalf("frame.ClampFirst(99) = %d, the tail does not fill the window", last)
	}
	if got := frame.ClampFirst(starts, total, window, -4); got != 0 {
		t.Fatalf("frame.ClampFirst(-4) = %d, want 0", got)
	}
	if prev := frame.ClampFirst(starts, total, window, last-1); prev != last-1 {
		t.Fatalf("one step before the end = %d, want %d", prev, last-1)
	}
}

// The strip at 60 columns with twelve tabs: the active tab is always visible,
// and activating a tab that is far to the left brings it into view.
func TestRevealedFirstKeepsActiveTabInWindow(t *testing.T) {
	tabs := make([]TabInfo, 12)
	for i := range tabs {
		tabs[i] = TabInfo{Name: "file" + string(rune('a'+i)) + ".go"}
	}
	for active := range tabs {
		tabs[active].Active = true
		first := frame.RevealedFirst(tabs, 0, 60, 0, active)
		s := frame.PlaceStrip(0, 60, tabs, first, icons.Plain)
		found := false
		for _, r := range s.Runs {
			if r.Kind == hitTab && r.Index == active {
				found = true
			}
		}
		if !found {
			t.Fatalf("active %d is not visible from first %d", active, first)
		}
		tabs[active].Active = false
	}
}

// A scroll that is already right does not move: revealing a visible tab keeps it.
func TestRevealedFirstDoesNotScrollWhenTheTabIsVisible(t *testing.T) {
	tabs := make([]TabInfo, 12)
	for i := range tabs {
		tabs[i] = TabInfo{Name: "t.go"}
	}
	if got := frame.RevealedFirst(tabs, 0, 60, 3, 4); got != 3 {
		t.Fatalf("frame.RevealedFirst(first 3, tab 4) = %d, want 3", got)
	}
	if got := frame.RevealedFirst(tabs, 0, 60, 3, 1); got != 1 {
		t.Fatalf("frame.RevealedFirst(first 3, tab 1) = %d, want 1", got)
	}
}
