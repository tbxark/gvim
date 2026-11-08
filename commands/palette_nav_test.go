package commands

import "testing"

// step is one call on a Palette: "select", "scroll" or "size".
type step struct {
	op string
	n  int
	m  int // second argument of "size": visible rows
}

func run(p *Palette, steps []step) {
	for _, s := range steps {
		switch s.op {
		case "select":
			p.Select(s.n)
		case "scroll":
			p.ScrollBy(s.n)
		case "size":
			p.SetSize(s.n, s.m)
		}
	}
}

// checkState fails the test unless p has the wanted highlight and window.
func checkState(t *testing.T, p *Palette, selected, first, count int) {
	t.Helper()
	if got := p.Selected(); got != selected {
		t.Errorf("Selected() = %d, want %d", got, selected)
	}
	if gf, gc := p.Window(); gf != first || gc != count {
		t.Errorf("Window() = (%d, %d), want (%d, %d)", gf, gc, first, count)
	}
}

func TestPaletteSelect(t *testing.T) {
	tests := []struct {
		name     string
		steps    []step
		selected int
		first    int // Window start
		count    int // Window length
	}{
		{
			name:     "no matches selects zero and shows nothing",
			steps:    []step{{op: "size", n: 0, m: 5}, {op: "select", n: 3}},
			selected: 0, first: 0, count: 0,
		},
		{
			name:     "fewer matches than rows",
			steps:    []step{{op: "size", n: 3, m: 10}, {op: "select", n: 2}},
			selected: 2, first: 0, count: 3,
		},
		{
			name:     "selecting past the view scrolls just enough",
			steps:    []step{{op: "size", n: 20, m: 5}, {op: "select", n: 7}},
			selected: 7, first: 3, count: 5,
		},
		{
			name:     "selecting back up scrolls the view up",
			steps:    []step{{op: "size", n: 20, m: 5}, {op: "select", n: 7}, {op: "select", n: 2}},
			selected: 2, first: 2, count: 5,
		},
		{
			name:     "selecting inside the view leaves it alone",
			steps:    []step{{op: "size", n: 20, m: 5}, {op: "scroll", n: 4}, {op: "select", n: 5}},
			selected: 5, first: 4, count: 5,
		},
		{
			name:     "negative index selects the first match",
			steps:    []step{{op: "size", n: 20, m: 5}, {op: "select", n: 9}, {op: "select", n: -4}},
			selected: 0, first: 0, count: 5,
		},
		{
			name:     "index past the end selects the last match",
			steps:    []step{{op: "size", n: 20, m: 5}, {op: "select", n: 99}},
			selected: 19, first: 15, count: 5,
		},
		{
			name:     "zero visible rows still tracks the selection",
			steps:    []step{{op: "size", n: 4, m: 0}, {op: "select", n: 2}},
			selected: 2, first: 2, count: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var p Palette
			run(&p, tc.steps)
			checkState(t, &p, tc.selected, tc.first, tc.count)
		})
	}
}

func TestPaletteScrollBy(t *testing.T) {
	tests := []struct {
		name     string
		steps    []step
		selected int
		first    int
		count    int
	}{
		{
			name:     "wheel down three notches of one row",
			steps:    []step{{op: "size", n: 20, m: 5}, {op: "scroll", n: 3}, {op: "scroll", n: 3}},
			selected: 0, first: 6, count: 5,
		},
		{
			name:     "wheel up past the top clamps to zero",
			steps:    []step{{op: "size", n: 20, m: 5}, {op: "scroll", n: 3}, {op: "scroll", n: -10}},
			selected: 0, first: 0, count: 5,
		},
		{
			name:     "wheel past the bottom clamps to the last full window",
			steps:    []step{{op: "size", n: 20, m: 5}, {op: "scroll", n: 100}},
			selected: 0, first: 15, count: 5,
		},
		{
			name:     "fewer matches than rows cannot scroll",
			steps:    []step{{op: "size", n: 3, m: 5}, {op: "scroll", n: 3}},
			selected: 0, first: 0, count: 3,
		},
		{
			name:     "scrolling leaves the highlighted match alone",
			steps:    []step{{op: "size", n: 20, m: 5}, {op: "select", n: 1}, {op: "scroll", n: 3}},
			selected: 1, first: 3, count: 5,
		},
		{
			name:     "scrolling up then selecting an out-of-view match",
			steps:    []step{{op: "size", n: 20, m: 5}, {op: "scroll", n: 10}, {op: "select", n: 0}},
			selected: 0, first: 0, count: 5,
		},
		{
			name:     "selecting a match still in view keeps the wheel position",
			steps:    []step{{op: "size", n: 20, m: 5}, {op: "scroll", n: 3}, {op: "select", n: 4}},
			selected: 4, first: 3, count: 5,
		},
		{
			name:     "selecting the first match after a scroll brings the view back",
			steps:    []step{{op: "size", n: 20, m: 5}, {op: "scroll", n: 6}, {op: "select", n: 0}},
			selected: 0, first: 0, count: 5,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var p Palette
			run(&p, tc.steps)
			checkState(t, &p, tc.selected, tc.first, tc.count)
		})
	}
}

func TestPaletteSetSizeClampsState(t *testing.T) {
	tests := []struct {
		name     string
		steps    []step
		selected int
		first    int
		count    int
	}{
		{
			name:     "shrinking the list pulls the selection in",
			steps:    []step{{op: "size", n: 20, m: 5}, {op: "select", n: 9}, {op: "size", n: 4, m: 5}},
			selected: 3, first: 0, count: 4,
		},
		{
			name:     "growing the list keeps the selection",
			steps:    []step{{op: "size", n: 20, m: 5}, {op: "select", n: 9}, {op: "size", n: 40, m: 5}},
			selected: 9, first: 5, count: 5,
		},
		{
			name:     "negative sizes count as none",
			steps:    []step{{op: "size", n: -2, m: -1}},
			selected: 0, first: 0, count: 0,
		},
		{
			name:     "a taller box shows more rows",
			steps:    []step{{op: "size", n: 20, m: 3}, {op: "select", n: 2}, {op: "size", n: 20, m: 8}},
			selected: 2, first: 0, count: 8,
		},
		{
			name:     "a shorter list clamps the selection and the view",
			steps:    []step{{op: "size", n: 20, m: 5}, {op: "select", n: 19}, {op: "size", n: 7, m: 5}},
			selected: 6, first: 2, count: 5,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var p Palette
			run(&p, tc.steps)
			checkState(t, &p, tc.selected, tc.first, tc.count)
		})
	}
}

func TestPaletteZeroValue(t *testing.T) {
	var p Palette
	checkState(t, &p, 0, 0, 0)
	p.ScrollBy(5)
	p.Select(3)
	checkState(t, &p, 0, 0, 0)
}

func TestPaletteWindowNeverPastLastMatch(t *testing.T) {
	// The window [2, 7) ends at the last of 7 matches however far the wheel goes.
	var p Palette
	run(&p, []step{{op: "size", n: 7, m: 5}, {op: "select", n: 6}, {op: "scroll", n: 50}})
	checkState(t, &p, 6, 2, 5)
}
