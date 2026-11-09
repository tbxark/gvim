package dock

import (
	"image/color"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
)

// styledSavedScreen is a styled snapshot of one row reading "OLD".
func styledSavedScreen() *terminal.SavedScreen {
	red := "#ff0000"
	cells := []terminal.SavedCell{
		{Rune: 'O', Fg: red, Bold: true},
		{Rune: 'L'},
		{Rune: 'D'},
	}
	return &terminal.SavedScreen{Cols: 3, Rows: []terminal.SavedRow{{Cells: cells}}}
}

// newScreen builds a fake live screen with rows of text.
func newScreen(width int, rows ...string) terminal.Screen {
	cells := make([]terminal.Cell, len(rows)*width)
	for r, text := range rows {
		for i, ch := range []rune(text) {
			cells[r*width+i] = terminal.Cell{Rune: ch}
		}
	}
	return terminal.NewScreen(len(rows), width, cells)
}

func TestPanelSavedStateCapturesStyledScreen(t *testing.T) {
	t.Parallel()
	term := newFakeTerm()
	term.screen = newScreen(6, "ab")
	term.screen = terminal.NewScreen(1, 6, []terminal.Cell{{Rune: 'a', Style: terminal.Style{Fg: color.RGBA{R: 255, A: 255}}}, {Rune: 'b'}})
	p := newTestPanel(t, &fakeStarter{term: term})
	st := p.SavedState()
	if st.Screen == nil || len(st.Screen.Rows) != 1 {
		t.Fatalf("captured screen = %+v", st.Screen)
	}
	cells := st.Screen.Rows[0].Cells
	if len(cells) < 2 || cells[0].Rune != 'a' || cells[0].Fg != "#ff0000" {
		t.Fatalf("captured cells = %+v", cells)
	}
}

func TestTabsRestoreAppliesStyledScreenTail(t *testing.T) {
	t.Parallel()
	var tabs Tabs
	tabs.SetHost(Host{
		RootDir:     func() string { return "/ws" },
		Notice:      func(string) {},
		ContentSize: func() (int, int) { return 80, 24 },
		Focus:       func() {},
	})
	tabs.SetPorts(Ports{Starter: &fakeStarter{term: newFakeTerm()}})
	tabs.Restore(terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true,
		Rows: []string{"OLD"}, Screen: func() *terminal.SavedScreen {
			bad := styledSavedScreen()
			bad.Rows[0].Cells = []terminal.SavedCell{{Rune: 'B'}, {Rune: 'A'}, {Rune: 'D'}}
			return bad
		}(),
		Tabs: []terminal.SavedTab{{
			Name: "t", Open: true, Rows: []string{"OLD"}, Screen: styledSavedScreen(), Live: true,
		}},
		ActiveTab: 0,
	})
	p := tabs.tabs[0]
	if len(p.prior) != 1 || p.prior[0].Cells[0].Style.Fg == nil || !p.prior[0].Cells[0].Style.Bold {
		t.Fatalf("restored prior = %+v", p.prior[0].Cells)
	}
	if p.prior[0].Text() != "OLD" {
		t.Fatalf("restored text = %q", p.prior[0].Text())
	}
	// A save of the restored (undived) tab keeps its styles.
	st := tabs.SavedState()
	if st.Screen == nil || st.Screen.Rows[0].Cells[0].Fg != "#ff0000" {
		t.Fatalf("re-captured screen = %+v", st.Screen)
	}
}

func TestRestoreIgnoresMismatchedScreen(t *testing.T) {
	t.Parallel()
	var tabs Tabs
	tabs.SetHost(Host{
		RootDir:     func() string { return "/ws" },
		Notice:      func(string) {},
		ContentSize: func() (int, int) { return 80, 24 },
		Focus:       func() {},
	})
	tabs.SetPorts(Ports{Starter: &fakeStarter{term: newFakeTerm()}})
	tabs.Restore(terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true,
		Tabs: []terminal.SavedTab{{
			Name: "t", Open: true, Rows: []string{"OLD"}, Screen: func() *terminal.SavedScreen {
				bad := styledSavedScreen()
				bad.Rows[0].Cells = []terminal.SavedCell{{Rune: 'B'}, {Rune: 'A'}, {Rune: 'D'}}
				return bad
			}(),
		}},
	})
	p := tabs.tabs[0]
	if got := p.prior[0].Text(); got != "OLD" {
		t.Fatalf("text = %q", got)
	}
	if p.prior[0].Cells[0].Style.Fg != nil || p.prior[0].Cells[0].Style.Bold {
		t.Fatalf("a mismatched snapshot was applied: %+v", p.prior[0].Cells)
	}
}

func TestRevivedTabPaintsSnapshotAboveNewOutput(t *testing.T) {
	t.Parallel()
	starter := &fakeStarter{term: newFakeTerm()}
	starter.term.screen = newScreen(20, "NEW")
	tabs := liveTestTabs(t, Effects{RestoreLive: true}, starter)
	tabs.Restore(terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true,
		Tabs: []terminal.SavedTab{{
			Name: "t", Open: true, Cwd: "/ws", LaunchDir: "/ws",
			Rows: []string{"OLD"}, Screen: styledSavedScreen(), Live: true,
		}},
		ActiveTab: 0,
	})
	tabs.SetActive(0)
	cmd := tabs.Sync()
	if cmd == nil {
		t.Fatal("the dive did not start")
	}
	finishStart(t, tabs, cmd())
	p := tabs.Active()
	if !p.Running() {
		t.Fatal("the tab did not revive")
	}
	// Enter the read-only view: the styled snapshot stays above the new output.
	applyScroll(t, p, p.ToggleScrollback())
	if p.combined() < 2 {
		t.Fatalf("combined rows = %d", p.combined())
	}
	// Scroll to the oldest row so the snapshot above the new output is visible.
	p.scroll.top = 0
	l := frame.Layout{DockOn: true, W: 80, H: 6, DockY: 0, DockH: 6}
	g := grid.New(80, 6)
	p.Draw(g, l, style.Styles{})
	if row := g.Plain()[1]; !strings.Contains(row, "OLD") {
		t.Fatalf("first body row = %q, want the snapshot", row)
	}
	oldStyle := g.At(0, 1).Style.FG
	if oldStyle == nil {
		t.Fatal("the snapshot lost its style")
	}
	if r, _, _, _ := oldStyle.RGBA(); r>>8 != 255 {
		t.Fatalf("snapshot fg = %v, want red", oldStyle)
	}
	if row := g.Plain()[2]; !strings.Contains(row, "NEW") {
		t.Fatalf("second body row = %q, want the new shell output below the snapshot", row)
	}
}

// TestRestoredLiveTabKeepsLiveFlagOnResave: a dormant live-restored tab saved again
// (or extracted) keeps its live flag, so the next restore can still revive it on the
// first dive; the dock must not turn a dormant live tab into a dead one.
func TestRestoredLiveTabKeepsLiveFlagOnResave(t *testing.T) {
	t.Parallel()
	var tabs Tabs
	tabs.SetHost(Host{
		RootDir:     func() string { return "/ws" },
		Notice:      func(string) {},
		ContentSize: func() (int, int) { return 80, 24 },
		Focus:       func() {},
	})
	tabs.SetPorts(Ports{Starter: &fakeStarter{term: newFakeTerm()}})
	tabs.Restore(terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Live: true, Rows: []string{"still here"},
		Tabs: []terminal.SavedTab{{
			Name: "t", Open: true, Cwd: "/ws", Rows: []string{"still here"}, Live: true,
		}},
	})
	st := tabs.SavedState()
	if !st.Live || !st.Tabs[0].Live {
		t.Fatalf("a dormant live tab was re-saved as dead: %+v", st)
	}
}

// remainderRecord builds a saved state with a styled history row, a styled
// visible row, a visible bar cursor and no alt screen.
func remainderRecord() terminal.SavedState {
	red := "#ff0000"
	screen := &terminal.SavedScreen{
		Cols: 6,
		History: []terminal.SavedRow{{Cells: []terminal.SavedCell{
			{Rune: 'h', Bold: true}, {Rune: 'i'},
		}}},
		Rows: []terminal.SavedRow{{Cells: []terminal.SavedCell{
			{Rune: 'o', Fg: red}, {Rune: 'l'}, {Rune: 'd'},
		}}},
		Cursor: terminal.SavedCursor{Row: 0, Col: 1, Visible: true, Shape: "bar"},
	}
	return terminal.SavedState{
		Version: terminal.SavedStateVersion, Open: true, Width: 6, Rows: []string{"hi", "old"},
		Screen: screen,
		Tabs: []terminal.SavedTab{{
			Name: "t", Open: true, Rows: []string{"hi", "old"}, Screen: screen, Live: true,
		}},
		ActiveTab: 0,
	}
}

// TestPanelCapturesHistoryCursorAndAlt checks the live capture writes the styled
// history window, the cursor and the alt flag.
func TestPanelCapturesHistoryCursorAndAlt(t *testing.T) {
	t.Parallel()
	term := newFakeTerm()
	term.styledHistory = []terminal.Line{{Cells: []terminal.Cell{{Rune: 'h', Style: terminal.Style{Bold: true}}}}}
	cell := terminal.Cell{Rune: 'x', Style: terminal.Style{Fg: color.RGBA{R: 255, A: 255}}}
	term.screen = terminal.NewScreen(1, 4, []terminal.Cell{cell})
	term.screen.Cursor = terminal.Cursor{Row: 0, Col: 1, Visible: true, Shape: "underline"}
	p := newTestPanel(t, &fakeStarter{term: term})
	st := p.SavedState()
	if st.Screen == nil || len(st.Screen.History) != 1 || len(st.Screen.Rows) != 1 {
		t.Fatalf("captured history/rows = %+v", st.Screen)
	}
	if st.Screen.Cursor != (terminal.SavedCursor{Row: 0, Col: 1, Visible: true, Shape: "underline"}) {
		t.Fatalf("captured cursor = %+v", st.Screen.Cursor)
	}
	// An alt screen has no history window and records the flag.
	term.screen.Alt = true
	st = p.SavedState()
	if st.Screen == nil || !st.Screen.Alt || len(st.Screen.History) != 0 {
		t.Fatalf("alt capture = %+v", st.Screen)
	}
}

// TestTabsRestoreHistoryAndCursor checks the styled history window and the saved
// cursor are applied to the restored transcript, painted in the read-only view,
// and re-saved intact.
func TestTabsRestoreHistoryAndCursor(t *testing.T) {
	t.Parallel()
	var tabs Tabs
	tabs.SetHost(Host{
		RootDir:     func() string { return "/ws" },
		Notice:      func(string) {},
		ContentSize: func() (int, int) { return 80, 24 },
		Focus:       func() {},
	})
	tabs.SetPorts(Ports{Starter: &fakeStarter{term: newFakeTerm()}})
	tabs.Restore(remainderRecord())
	p := tabs.tabs[0]
	if len(p.prior) != 2 {
		t.Fatalf("prior = %d", len(p.prior))
	}
	if !p.prior[0].Cells[0].Style.Bold {
		t.Fatalf("history row lost its style: %+v", p.prior[0].Cells)
	}
	if p.prior[1].Cells[0].Style.Fg == nil {
		t.Fatalf("visible row lost its style: %+v", p.prior[1].Cells)
	}
	if p.savedVisibleRows != 1 || p.savedCursor != (terminal.SavedCursor{Row: 0, Col: 1, Visible: true, Shape: "bar"}) {
		t.Fatalf("saved metadata = %d %+v", p.savedVisibleRows, p.savedCursor)
	}
	// The read-only view paints the history styles and the saved cursor block.
	l := frame.Layout{DockOn: true, W: 80, H: 6, DockY: 0, DockH: 6}
	g := grid.New(80, 6)
	p.Draw(g, l, style.Styles{})
	if !g.At(0, 1).Style.Bold {
		t.Fatalf("history row not painted styled: %+v", g.At(0, 1))
	}
	if !g.At(1, 2).Style.Reverse {
		t.Fatalf("saved cursor not painted at (1,2): %+v", g.At(1, 2))
	}
	// A re-save keeps the full snapshot.
	st := tabs.SavedState()
	if st.Screen == nil || len(st.Screen.History) != 1 || len(st.Screen.Rows) != 1 || st.Screen.Cursor != p.savedCursor {
		t.Fatalf("re-saved snapshot = %+v", st.Screen)
	}
}

// TestRestoreIgnoresCorruptRemainder checks a mismatched history text, an invalid
// cursor or an impossible history length ignores the whole snapshot, leaving the
// old plain transcript behavior.
func TestRestoreIgnoresCorruptRemainder(t *testing.T) {
	t.Parallel()
	corrupt := func(mutate func(*terminal.SavedScreen)) terminal.SavedState {
		st := remainderRecord()
		mutate(st.Screen)
		return st
	}
	cases := map[string]terminal.SavedState{
		"history text mismatch": corrupt(func(s *terminal.SavedScreen) {
			s.History[0].Cells = []terminal.SavedCell{{Rune: 'z'}}
		}),
		"cursor out of range": corrupt(func(s *terminal.SavedScreen) {
			s.Cursor = terminal.SavedCursor{Row: 9, Col: 0, Visible: true}
		}),
		"unknown cursor shape": corrupt(func(s *terminal.SavedScreen) {
			s.Cursor.Shape = "star"
		}),
		"history too long": corrupt(func(s *terminal.SavedScreen) {
			s.History = make([]terminal.SavedRow, terminal.MaxSavedHistoryRows+1)
		}),
	}
	for name, state := range cases {
		var tabs Tabs
		tabs.SetHost(Host{RootDir: func() string { return "/ws" }, Notice: func(string) {}, ContentSize: func() (int, int) { return 80, 24 }, Focus: func() {}})
		tabs.SetPorts(Ports{Starter: &fakeStarter{term: newFakeTerm()}})
		tabs.Restore(state)
		p := tabs.tabs[0]
		if p.savedVisibleRows != 0 || p.savedCursor != (terminal.SavedCursor{}) || p.savedAlt {
			t.Fatalf("%s: corrupt snapshot metadata survived: %d %+v", name, p.savedVisibleRows, p.savedCursor)
		}
		if p.prior[0].Cells[0].Style.Bold || p.prior[1].Cells[0].Style.Fg != nil {
			t.Fatalf("%s: corrupt snapshot painted styles: %+v", name, p.prior)
		}
	}
}

// TestRestoreAltAndReflowInvalidates checks the alt flag round-trips and that a
// reflow drops the snapshot row metadata (the mapping no longer holds).
func TestRestoreAltAndReflowInvalidates(t *testing.T) {
	t.Parallel()
	st := remainderRecord()
	st.Screen.Alt = true
	st.Screen.History = nil
	st.Screen.Rows = []terminal.SavedRow{{Cells: []terminal.SavedCell{{Rune: 'o'}}}}
	st.Width = 6
	st.Rows = []string{"o"}
	st.Tabs[0].Screen = st.Screen
	st.Tabs[0].Rows = []string{"o"}
	st.Tabs[0].Width = 6
	var tabs Tabs
	tabs.SetHost(Host{RootDir: func() string { return "/ws" }, Notice: func(string) {}, ContentSize: func() (int, int) { return 80, 24 }, Focus: func() {}})
	tabs.SetPorts(Ports{Starter: &fakeStarter{term: newFakeTerm()}})
	tabs.Restore(st)
	p := tabs.tabs[0]
	if !p.savedAlt {
		t.Fatal("alt flag was not restored")
	}
	p.reflowPrior(40)
	if p.savedVisibleRows != 0 || p.savedAlt || p.savedCursor != (terminal.SavedCursor{}) {
		t.Fatalf("reflow kept stale snapshot metadata: %+v", p.savedCursor)
	}
}

// TestRevivedTabStopsPaintingTheSavedCursor checks the saved cursor belongs to the
// read-only restored view: once the shell is revived, the live cursor is the only
// cursor (the old block is not painted anymore).
func TestRevivedTabStopsPaintingTheSavedCursor(t *testing.T) {
	t.Parallel()
	starter := &fakeStarter{term: newFakeTerm()}
	starter.term.screen = newScreen(20, "NEW")
	tabs := liveTestTabs(t, Effects{RestoreLive: true}, starter)
	tabs.Restore(remainderRecord())
	tabs.SetActive(0)
	cmd := tabs.Sync()
	if cmd == nil {
		t.Fatal("the dive did not start")
	}
	finishStart(t, tabs, cmd())
	p := tabs.Active()
	applyScroll(t, p, p.ToggleScrollback())
	p.scroll.top = 0
	l := frame.Layout{DockOn: true, W: 80, H: 6, DockY: 0, DockH: 6}
	g := grid.New(80, 6)
	p.Draw(g, l, style.Styles{})
	if g.At(1, 2).Style.Reverse {
		t.Fatal("the saved cursor is still painted after the revive")
	}
	// The styled history row of the prior transcript is still painted after the
	// revive (only the cursor belongs to the read-only view).
	if !g.At(0, 1).Style.Bold {
		t.Fatalf("the revived tab lost its styled history row: %+v", g.At(0, 1))
	}
}

// TestPanelCapturesAndRestoresTitle checks the OSC 0/2 title: captured with the
// tab, restored as the plain-shell tab label (behind an explicit rename and the
// launch record), re-saved, and cleaned/bounded.
func TestPanelCapturesAndRestoresTitle(t *testing.T) {
	t.Parallel()
	term := newFakeTerm()
	term.title = "vim main.go"
	term.screen = newScreen(20, "out")
	p := newTestPanel(t, &fakeStarter{term: term})
	for range 2 {
		st := p.SavedState()
		if st.Title != "vim main.go" {
			t.Fatalf("captured title = %q", st.Title)
		}
	}
	restore := func(title string, launch []string) *Tabs {
		var tabs Tabs
		tabs.SetHost(Host{RootDir: func() string { return "/ws" }, Notice: func(string) {}, ContentSize: func() (int, int) { return 80, 24 }, Focus: func() {}})
		tabs.SetPorts(Ports{Starter: &fakeStarter{term: newFakeTerm()}})
		tabs.Restore(terminal.SavedState{
			Version: terminal.SavedStateVersion, Open: true, Title: title, Rows: []string{"out"},
			LaunchArgv: launch,
			Tabs: []terminal.SavedTab{{
				Name: "", Open: true, Title: title, Rows: []string{"out"}, LaunchArgv: launch,
			}},
		})
		return &tabs
	}
	restored := restore("vim main.go", nil)
	if got := restored.TabNames()[0]; !strings.Contains(got, "vim main.go") {
		t.Fatalf("restored tab label = %q, want the recorded title", got)
	}
	if st := restored.SavedState(); st.Title != "vim main.go" || st.Tabs[0].Title != "vim main.go" {
		t.Fatalf("re-saved title = %q / %q", st.Title, st.Tabs[0].Title)
	}
	// An explicit rename still wins.
	restored.RenameActive("mine")
	if got := restored.TabNames()[0]; !strings.Contains(got, "mine") || strings.Contains(got, "vim") {
		t.Fatalf("renamed label = %q", got)
	}
	// A launch record keeps its program name over the OSC title.
	launch := restore("some title", []string{"/bin/bash"})
	if got := launch.TabNames()[0]; !strings.Contains(got, "bash") || strings.Contains(got, "some title") {
		t.Fatalf("launch label = %q", got)
	}
	// Capture cleans controls and bounds the title.
	bad := newFakeTerm()
	bad.title = "\033]0;bad\007\n" + strings.Repeat("x", terminal.MaxTitleRunes+10)
	bad.screen = newScreen(20, "out")
	badPanel := newTestPanel(t, &fakeStarter{term: bad})
	got := badPanel.SavedState().Title
	if strings.ContainsAny(got, "\x1b\n\x00") || len([]rune(got)) > terminal.MaxTitleRunes {
		t.Fatalf("unclean title = %q (%d runes)", got, len([]rune(got)))
	}
}

// TestAltScreenContentAndTitleRoundTrip checks the alternate screen's content is
// captured first-class (alt flag, styled rows, cursor, no history) and restored into
// the read-only prior transcript with the recorded title; a corrupt alt frame with a
// history window is ignored whole but the title still restores.
func TestAltScreenContentAndTitleRoundTrip(t *testing.T) {
	t.Parallel()
	red := color.RGBA{R: 255, A: 255}
	term := newFakeTerm()
	term.title = "alt-app"
	screen := terminal.NewScreen(2, 6, []terminal.Cell{
		{Rune: 'A', Style: terminal.Style{Fg: red, Bold: true}},
		{Rune: 'L'}, {Rune: 'T'},
		{Rune: 'x'},
	})
	screen.Alt = true
	screen.Cursor = terminal.Cursor{Row: 1, Col: 1, Visible: true, Shape: "bar"}
	term.screen = screen
	p := newTestPanel(t, &fakeStarter{term: term})
	st := p.SavedState()
	if st.Screen == nil || !st.Screen.Alt || len(st.Screen.History) != 0 {
		t.Fatalf("captured alt frame = %+v", st.Screen)
	}
	if len(st.Screen.Rows) != 2 || st.Screen.Rows[0].Cells[0].Rune != 'A' || !st.Screen.Rows[0].Cells[0].Bold {
		t.Fatalf("captured alt content = %+v", st.Screen.Rows)
	}
	if st.Title != "alt-app" {
		t.Fatalf("captured alt title = %q", st.Title)
	}
	var tabs Tabs
	tabs.SetHost(Host{RootDir: func() string { return "/ws" }, Notice: func(string) {}, ContentSize: func() (int, int) { return 80, 24 }, Focus: func() {}})
	tabs.SetPorts(Ports{Starter: &fakeStarter{term: newFakeTerm()}})
	tabs.Restore(st)
	panel := tabs.tabs[0]
	if !panel.savedAlt {
		t.Fatal("restored alt flag was lost")
	}
	if len(panel.prior) != 2 || panel.prior[0].Cells[0].Rune != 'A' || panel.prior[0].Cells[0].Style.Fg == nil {
		t.Fatalf("restored alt content = %+v", panel.prior)
	}
	if got := tabs.TabNames()[0]; !strings.Contains(got, "alt-app") {
		t.Fatalf("restored alt tab label = %q", got)
	}
	if st2 := tabs.SavedState(); st2.Screen == nil || !st2.Screen.Alt || st2.Title != "alt-app" || len(st2.Screen.Rows) == 0 {
		t.Fatalf("re-saved alt frame = %+v", st2.Screen)
	}

	// A corrupt alt frame (history window) is ignored whole, the title still restores.
	corrupt := st
	corrupt.Screen = &terminal.SavedScreen{
		Cols:    6,
		Alt:     true,
		History: []terminal.SavedRow{{Cells: []terminal.SavedCell{{Rune: 'h'}}}},
		Rows:    []terminal.SavedRow{{Cells: []terminal.SavedCell{{Rune: 'A'}}}},
	}
	corrupt.Rows = []string{"out"}
	for i := range corrupt.Tabs {
		corrupt.Tabs[i].Screen = corrupt.Screen
		corrupt.Tabs[i].Rows = []string{"out"}
	}
	var bad Tabs
	bad.SetHost(Host{RootDir: func() string { return "/ws" }, Notice: func(string) {}, ContentSize: func() (int, int) { return 80, 24 }, Focus: func() {}})
	bad.SetPorts(Ports{Starter: &fakeStarter{term: newFakeTerm()}})
	bad.Restore(corrupt)
	badPanel := bad.tabs[0]
	if badPanel.savedAlt || badPanel.prior[0].Cells[0].Style.Fg != nil {
		t.Fatal("a corrupt alt frame was applied")
	}
	if badPanel.titleText != "alt-app" {
		t.Fatalf("the title was lost with the corrupt alt frame: %q", badPanel.titleText)
	}
}
