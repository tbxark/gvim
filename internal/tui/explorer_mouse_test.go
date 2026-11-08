package tui

import (
	"strconv"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/explorer"
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
	m.Update(tea.MouseWheelMsg(tea.Mouse{X: 5, Y: m.layout().ExBodyTop, Button: tea.MouseWheelDown}))
	body := m.explorerBodyRows()
	off, pins := explorer.Viewport(tr.Rows(), tr.ScrollOffset(), body)
	if off != wheelRows && off != explorer.MaxScroll(tr.Rows(), body) {
		t.Fatalf("setup: offset %d after one wheel notch, want %d (or the ceiling)", off, wheelRows)
	}
	// The pinned ancestors come first; the row at the top of the run is the one
	// the click must act on.
	top := m.layout().ExBodyTop + len(pins)
	_, cmd := m.Update(click(5, top, tea.MouseLeft))
	if tr.selected != off {
		t.Fatalf("click on the first run row selected row %d, want %d (f%d)", tr.selected, off, off)
	}
	if got, ok := runMsg(cmd).(expanel.OpenFileMsg); !ok || got.Path != "/ws/f"+strconv.Itoa(off) {
		t.Errorf("click sent %#v, want OpenFileMsg for /ws/f%d", runMsg(cmd), off)
	}
}
