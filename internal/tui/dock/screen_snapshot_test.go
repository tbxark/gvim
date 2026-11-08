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
