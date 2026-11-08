package tui

import (
	"maps"
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/theme"
)

// newSplitModelKeys is newSplitModel with the test bindings plus extra, so a test can
// press a key that runs one of the split actions.
func newSplitModelKeys(t *testing.T, f *multiFake, w, h int, extra fakeKeymap) *Model {
	t.Helper()
	keys := testKeys()
	maps.Copy(keys, extra)
	m := New(Deps{Editor: f, Keys: keys, Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

// paneRects returns the rectangle of every pane of the layout, in reading order.
func paneRects(l layout) []rect {
	out := make([]rect, len(l.Panes))
	for i, p := range l.Panes {
		out[i] = p.Rect
	}
	return out
}

// ratioKeys binds the chords that run the two resize actions.
var ratioKeys = fakeKeymap{
	"normal|l|ctrl": {Action: "increase_split_size"},
	"normal|k|ctrl": {Action: "decrease_split_size"},
}

func TestResizeMovesTheRatioOfTheFocusedSplit(t *testing.T) {
	// A side-by-side split of the 80 by 21 band: the first child takes the ratio
	// percent of the 79 cells left by the line. At 50 the pane widths are 40 and 39.
	tests := []struct {
		name   string
		setup  []string // actions run before the resize, which leave the focus on the pane under test
		action string
		ratio  int // the ratio of the split after the action
		panes  []rect
	}{
		{
			name:   "the focused second pane grows",
			setup:  []string{"split_vertical"},
			action: "increase_split_size",
			ratio:  45,
			panes:  []rect{{X: 0, Y: 2, W: 36, H: 21}, {X: 37, Y: 2, W: 43, H: 21}},
		},
		{
			name:   "the focused second pane shrinks",
			setup:  []string{"split_vertical"},
			action: "decrease_split_size",
			ratio:  55,
			panes:  []rect{{X: 0, Y: 2, W: 43, H: 21}, {X: 44, Y: 2, W: 36, H: 21}},
		},
		{
			name:   "the focused first pane grows",
			setup:  []string{"split_vertical", "next_split"},
			action: "increase_split_size",
			ratio:  55,
			panes:  []rect{{X: 0, Y: 2, W: 43, H: 21}, {X: 44, Y: 2, W: 36, H: 21}},
		},
		{
			name:   "the focused first pane shrinks",
			setup:  []string{"split_vertical", "next_split"},
			action: "decrease_split_size",
			ratio:  45,
			panes:  []rect{{X: 0, Y: 2, W: 36, H: 21}, {X: 37, Y: 2, W: 43, H: 21}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newSplitModel(t, newMultiFake(2, 30), 80, 24)
			for _, a := range tt.setup {
				runAction(t, m, a)
			}
			focus := m.splits.Focus
			runAction(t, m, tt.action)
			if got := m.splits.Root.Share(); got != tt.ratio {
				t.Errorf("ratio = %d, want %d", got, tt.ratio)
			}
			if m.splits.Focus != focus {
				t.Errorf("the resize moved the focus")
			}
			if got := paneRects(m.layout()); !slices.Equal(got, tt.panes) {
				t.Errorf("panes = %v, want %v", got, tt.panes)
			}
		})
	}
}

func TestResizeStopsAtTheRatioBounds(t *testing.T) {
	tests := []struct {
		name  string
		setup []string
		ratio int    // the ratio after ten presses in the same direction
		panes []rect // the panes at that ratio
	}{
		{
			name:  "a focused second pane stops at 10 percent",
			setup: []string{"split_vertical"},
			ratio: 10,
			panes: []rect{{X: 0, Y: 2, W: 8, H: 21}, {X: 9, Y: 2, W: 71, H: 21}},
		},
		{
			name:  "a focused first pane stops at 90 percent",
			setup: []string{"split_vertical", "next_split"},
			ratio: 90,
			panes: []rect{{X: 0, Y: 2, W: 71, H: 21}, {X: 72, Y: 2, W: 8, H: 21}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newSplitModel(t, newMultiFake(2, 30), 80, 24)
			for _, a := range tt.setup {
				runAction(t, m, a)
			}
			for range 10 {
				runAction(t, m, "increase_split_size")
			}
			if got := m.splits.Root.Share(); got != tt.ratio {
				t.Errorf("ratio = %d, want %d", got, tt.ratio)
			}
			if got := paneRects(m.layout()); !slices.Equal(got, tt.panes) {
				t.Errorf("panes = %v, want %v", got, tt.panes)
			}
		})
	}
}

func TestResizeTouchesOnlyTheSplitThatHoldsTheFocus(t *testing.T) {
	// split_vertical puts the focus on the right pane, and split_horizontal splits that
	// pane again, so the focus is the bottom of the right column. Growing it changes the
	// stacked split's ratio and leaves the root split at 50.
	m := newSplitModel(t, newMultiFake(2, 30), 80, 24)
	runAction(t, m, "split_vertical")
	runAction(t, m, "split_horizontal")
	runAction(t, m, "increase_split_size")

	if got := m.splits.Root.Share(); got != 50 {
		t.Errorf("root ratio = %d, want 50", got)
	}
	inner := m.splits.Root.B
	if inner.Dir != stacked || inner.Share() != 45 {
		t.Errorf("inner split dir %v ratio %d, want stacked at 45", inner.Dir, inner.Share())
	}
	want := []rect{{X: 0, Y: 2, W: 40, H: 21}, {X: 41, Y: 2, W: 39, H: 9}, {X: 41, Y: 12, W: 39, H: 11}}
	if got := paneRects(m.layout()); !slices.Equal(got, want) {
		t.Errorf("panes = %v, want %v", got, want)
	}
}

func TestResizeWithOnePaneSaysSo(t *testing.T) {
	m := newSplitModel(t, newMultiFake(2, 30), 80, 24)
	runAction(t, m, "increase_split_size")
	if m.notice != "Only one split is open" {
		t.Errorf("notice = %q, want the one split notice", m.notice)
	}
	if got := paneRects(m.layout()); !slices.Equal(got, []rect{{X: 0, Y: 2, W: 80, H: 21}}) {
		t.Errorf("panes = %v, want the whole band", got)
	}
}

func TestResizeKeyReachesTheSplit(t *testing.T) {
	m := newSplitModelKeys(t, newMultiFake(2, 30), 80, 24, ratioKeys)
	runAction(t, m, "split_vertical")
	press(m, ctrl('l'))
	if got := m.splits.Root.Share(); got != 45 {
		t.Errorf("ratio after ctrl+l = %d, want 45", got)
	}
	press(m, ctrl('k'))
	press(m, ctrl('k'))
	if got := m.splits.Root.Share(); got != 55 {
		t.Errorf("ratio after two ctrl+k = %d, want 55", got)
	}
}

func TestResizeKeyFallsThroughWhenTheExplorerHasFocus(t *testing.T) {
	// The explorer keymap binds no resize key, so ctrl+l keeps its Normal binding, as
	// Fresh's application-wide actions do.
	f := newMultiFake(2, 30)
	keys := testKeys()
	maps.Copy(keys, ratioKeys)
	m := New(Deps{Editor: f, Explorer: newFakeTree(3), Keys: keys, Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	runAction(t, m, "split_vertical")
	press(m, ctrl('e')) // focus_file_explorer
	if !m.explorerFocus {
		t.Fatal("the explorer does not have the focus")
	}
	press(m, ctrl('l'))
	if got := m.splits.Root.Share(); got != 45 {
		t.Errorf("ratio with the explorer focused = %d, want 45", got)
	}
}
