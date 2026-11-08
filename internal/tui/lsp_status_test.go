package tui

import (
	"testing"

	"github.com/tbxark/gvim/internal/tui/list"
)

// overlayList returns the list view on top of the overlay stack.
func overlayList(t *testing.T, m *Model) *list.View {
	t.Helper()
	for i := len(m.overlays) - 1; i >= 0; i-- {
		if v, ok := m.overlays[i].(*list.View); ok {
			return v
		}
	}
	t.Fatal("no list view on the overlay stack")
	return nil
}
