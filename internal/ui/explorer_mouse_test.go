package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/theme"
	expanel "github.com/tbxark/gvim/internal/ui/explorer"
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
	m.Update(tea.MouseWheelMsg(tea.Mouse{X: 5, Y: explorerBodyTop, Button: tea.MouseWheelDown}))
	if off := tr.ScrollOffset(); off != wheelRows {
		t.Fatalf("setup: offset %d after one wheel notch, want %d", off, wheelRows)
	}
	_, cmd := m.Update(click(5, explorerBodyTop, tea.MouseLeft))
	if tr.selected != wheelRows {
		t.Fatalf("click on the top body row selected row %d, want %d (f%d)", tr.selected, wheelRows, wheelRows)
	}
	if got, ok := runMsg(cmd).(expanel.OpenFileMsg); !ok || got.Path != "/ws/f3" {
		t.Errorf("click sent %#v, want OpenFileMsg for /ws/f3", runMsg(cmd))
	}
}
