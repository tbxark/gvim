package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/theme"
)

// TestOpenCompareShowsTwoTabsAsTheCompareView checks the exported entry point that
// the --diff flag uses. It opens the compare view only when both tabs exist.
func TestOpenCompareShowsTwoTabsAsTheCompareView(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		tabs          int
		left, right   int
		wantOK        bool
		wantComparing bool
	}{
		{name: "two tabs", tabs: 2, left: 0, right: 1, wantOK: true, wantComparing: true},
		{name: "one tab", tabs: 1, left: 0, right: 1, wantOK: false, wantComparing: false},
		{name: "right tab missing", tabs: 2, left: 0, right: 5, wantOK: false, wantComparing: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ed := newFakeEditor("x")
			ed.tabs = make([]TabInfo, tt.tabs)
			for i := range ed.tabs {
				ed.tabs[i].Name = "f"
			}
			m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
			if got := m.OpenCompare(tt.left, tt.right); got != tt.wantOK {
				t.Fatalf("OpenCompare() = %v, want %v", got, tt.wantOK)
			}
			if got := m.Comparing(); got != tt.wantComparing {
				t.Fatalf("Comparing() = %v, want %v", got, tt.wantComparing)
			}
		})
	}
}
