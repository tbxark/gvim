package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	expanel "github.com/tbxark/gvim/internal/tui/explorer"
	"github.com/tbxark/gvim/theme"
)

// TestClickAfterWheelActsOnTheRowClicked scrolls the tree with the wheel and then
// clicks the top body row. The press is resolved against the scrolled tree, so the
// row acted on is the one drawn there, and focusing the panel does not scroll the
// view back to the selected row.
func TestClickAfterWheelActsOnTheRowClicked(t *testing.T) {
	t.Parallel()
	tr := newFakeTree(20)
	m := New(Deps{
		Editor: newFakeEditor("hello"), Explorer: tr, ShowExplorer: true,
		Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock,
	})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	top := m.layout().ExBodyTop
	m.Update(tea.MouseWheelMsg(tea.Mouse{X: 5, Y: top, Button: tea.MouseWheelDown}))
	if off := tr.ScrollOffset(); off != wheelRows {
		t.Fatalf("setup: offset %d after one wheel notch, want %d", off, wheelRows)
	}
	_, cmd := m.Update(click(5, top, tea.MouseLeft))
	if tr.selected != wheelRows {
		t.Fatalf("click on the top body row selected row %d, want %d (f%d)", tr.selected, wheelRows, wheelRows)
	}
	if got, ok := runMsg(cmd).(expanel.OpenFileMsg); !ok || got.Path != "/ws/f3" {
		t.Errorf("click sent %#v, want OpenFileMsg for /ws/f3", runMsg(cmd))
	}
}
