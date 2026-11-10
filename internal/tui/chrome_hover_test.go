package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/theme"
)

// TestChromeHoverFollowsTheMotion checks the three pointer-feedback areas the root
// tracks: the tab strip, the clickable status row and the explorer's resize grip.
// Each one is read from the hit map, so the feedback and the click target agree.
func TestChromeHoverFollowsTheMotion(t *testing.T) {
	th, err := theme.Load("dark")
	if err != nil {
		t.Fatal(err)
	}
	m := New(Deps{Editor: newFakeEditor("hello"), Explorer: newFakeTree(3), Keys: testKeys(), Theme: th, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.explorerOpen = true
	l := m.layout()

	tab := firstRun(l.Strip.Runs, hitTab)
	if tab.W == 0 {
		t.Fatalf("strip runs = %+v, want a tab", l.Strip.Runs)
	}
	m.Update(tea.MouseMotionMsg{X: tab.X + 1, Y: l.StripY})
	if m.stripHover.Kind != hitTab || m.stripHover.Index != tab.Index {
		t.Fatalf("strip hover = %+v, want the tab at %d", m.stripHover, tab.X)
	}
	if cell := m.paint().At(tab.X+1, l.StripY); !cell.Style.Underline {
		t.Fatalf("the hovered tab is not underlined: %+v", cell.Style)
	}

	seg := firstStatus(l.Status, frame.StatusSegmentEOL)
	if seg.W == 0 {
		t.Fatalf("status runs = %+v, want the line ending", l.Status)
	}
	m.Update(tea.MouseMotionMsg{X: seg.X + 1, Y: l.StatusY})
	if m.statusHover.Kind != hitStatusSegment || m.statusHover.Index != seg.Index {
		t.Fatalf("status hover = %+v, want the line ending segment", m.statusHover)
	}
	if cell := m.paint().At(seg.X+1, l.StatusY); !cell.Style.Underline {
		t.Fatalf("the hovered line ending is not underlined: %+v", cell.Style)
	}

	gripX := l.ExX + l.Ex - 1
	m.Update(tea.MouseMotionMsg{X: gripX, Y: l.ExBodyTop})
	if !m.gripHover {
		t.Fatalf("the grip at (%d, %d) is not hovered", gripX, l.ExBodyTop)
	}
	accent := style.Cell(m.styles.Accent).BG
	if got := m.paint().At(gripX, l.ExBodyTop).Style.FG; got != accent {
		t.Fatalf("grip ink = %v, want the accent %v", got, accent)
	}
	m.Update(tea.MouseMotionMsg{X: l.ExX + 2, Y: l.ExBodyTop})
	if m.gripHover {
		t.Fatal("the grip stayed hovered with the pointer off the border")
	}
	if got := m.paint().At(gripX, l.ExBodyTop).Style.FG; got == accent {
		t.Fatalf("the grip kept the accent ink after the pointer left: %v", got)
	}
}

// firstRun returns the first run of kind, or the zero run.
func firstRun(runs []frame.Run, kind frame.HitKind) frame.Run {
	for _, r := range runs {
		if r.Kind == kind {
			return r
		}
	}
	return frame.Run{}
}

// firstStatus returns the status run of segment index, or the zero run.
func firstStatus(runs []frame.Run, index int) frame.Run {
	for _, r := range runs {
		if r.Kind == frame.HitStatusSegment && r.Index == index {
			return r
		}
	}
	return frame.Run{}
}
