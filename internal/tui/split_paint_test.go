package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/theme"
)

// newSplitModelTheme is newSplitModel over the bundled theme name, so the paint can
// read a real split colour.
func newSplitModelTheme(t *testing.T, f *multiFake, name string) *Model {
	t.Helper()
	th, err := theme.Load(name)
	if err != nil {
		t.Fatal(err)
	}
	m := New(Deps{Editor: f, Keys: testKeys(), Theme: th, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

func TestSeparatorsDrawInTheSplitColour(t *testing.T) {
	want := style.Color(mustLoadTheme(t, "dracula"), "ui", "split_separator_fg")
	if want == nil {
		t.Fatal("the dracula theme has no split_separator_fg")
	}
	tests := []struct {
		name   string
		action string
		x, y   int // a cell on the line
		glyph  string
	}{
		{"a side-by-side line", "split_vertical", 40, 5, splitVLine},
		{"a stacked line", "split_horizontal", 10, 13, splitHLine},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newSplitModelTheme(t, newMultiFake(2, 30), "dracula")
			runAction(t, m, tt.action)
			cell := m.paint().At(tt.x, tt.y)
			if cell.Text != tt.glyph {
				t.Fatalf("cell (%d, %d) = %q, want the line %q", tt.x, tt.y, cell.Text, tt.glyph)
			}
			if cell.Style.FG != want {
				t.Errorf("line colour = %v, want the split colour %v", cell.Style.FG, want)
			}
		})
	}
}

// mustLoadTheme loads the bundled theme name, and fails the test when it is missing.
func mustLoadTheme(t *testing.T, name string) theme.Theme {
	t.Helper()
	th, err := theme.Load(name)
	if err != nil {
		t.Fatal(err)
	}
	return th
}
