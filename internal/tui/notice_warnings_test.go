package tui

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/theme"
)

// rootPaletteRun runs action the way the palette does, as a RunCommandMsg that Update
// handles, and returns the command the run started.
func rootPaletteRun(m *Model, action string) tea.Cmd {
	_, cmd := m.Update(palette.RunCommandMsg{Action: action})
	return cmd
}

// overlayRows returns the rows of the list overlay on top, and false when the top
// overlay is not a list.
func overlayRows(m *Model) ([]string, bool) {
	if len(m.overlays) == 0 {
		return nil, false
	}
	o, ok := m.overlays[len(m.overlays)-1].(listOverlay)
	if !ok {
		return nil, false
	}
	return rowsOf(o.v.rows), true
}

// TestShowWarningsListsTheNoticesOfTheSessionNewestFirst checks that every notice the
// root showed is kept, the one the app gave at start included, and that Show Warnings
// lists them with the newest first.
func TestShowWarningsListsTheNoticesOfTheSessionNewestFirst(t *testing.T) {
	t.Parallel()
	m := New(Deps{
		Editor: newFakeEditor("x"), Keys: testKeys(), Theme: theme.Theme{},
		Clock: testClock, Notice: "config: bad key",
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	rootPaletteRun(m, "list_macros")

	rootPaletteRun(m, "show_warnings")
	rows, ok := overlayRows(m)
	if !ok {
		t.Fatal("show_warnings did not open a list overlay")
	}
	sameLines(t, "warnings", rows, []string{
		"No macro recorded=",
		"config: bad key=",
	})
}

// TestShowWarningsWithNoWarningsSaysSo checks that an empty log opens no list and
// shows a notice instead.
func TestShowWarningsWithNoWarningsSaysSo(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, newFakeEditor("x"), nil, 80, 24)
	rootPaletteRun(m, "show_warnings")
	if _, ok := overlayRows(m); ok {
		t.Fatal("show_warnings opened a list with no warnings")
	}
	if m.notice != "No warnings this session" {
		t.Fatalf("notice = %q, want the empty-log notice", m.notice)
	}
}

// TestClearWarningsEmptiesTheLog checks that Clear Warnings drops the kept notices, so
// Show Warnings lists only what came after the clear.
func TestClearWarningsEmptiesTheLog(t *testing.T) {
	t.Parallel()
	m := newTestModel(t, newFakeEditor("x"), nil, 80, 24)
	rootPaletteRun(m, "list_macros")
	rootPaletteRun(m, "clear_warnings")
	if m.notice != "Warnings cleared" {
		t.Fatalf("notice = %q, want the cleared notice", m.notice)
	}

	rootPaletteRun(m, "show_warnings")
	if _, ok := overlayRows(m); ok {
		t.Fatal("show_warnings still lists the warnings that were cleared")
	}
}

// TestWarningLogSkipsRepeatsInARow checks that a notice which shows again straight
// after it was kept is not kept twice, so a key that fails again and again does not
// fill the list. A notice that came back after another one is kept again.
func TestWarningLogSkipsRepeatsInARow(t *testing.T) {
	t.Parallel()
	var l noticeLog
	prev := ""
	for _, shown := range []string{"a", "", "a", "a", "b", "b", "a"} {
		l.record(prev, shown)
		prev = shown
	}
	if got, want := l.items, []string{"a", "b", "a"}; !slices.Equal(got, want) {
		t.Fatalf("items = %q, want %q", got, want)
	}
}
