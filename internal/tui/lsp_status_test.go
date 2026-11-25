package tui

import (
	"slices"
	"testing"

	"github.com/tbxark/gvim/internal/tui/list"
)

// overlayList returns the list view on top of the overlay stack.
func overlayList(t *testing.T, m *Model) *list.View {
	t.Helper()
	for _, v := range slices.Backward(m.overlays) {
		if v, ok := v.(*list.View); ok {
			return v
		}
	}
	t.Fatal("no list view on the overlay stack")
	return nil
}
