package explorer

import "testing"

// TestWidthCols checks Fresh's ExplorerWidth rendering: a percentage of the frame
// (integer division), an exact column count, and no columns for the automatic
// width.
func TestWidthCols(t *testing.T) {
	tests := []struct {
		name  string
		width Width
		frame int
		want  int
	}{
		{"automatic", Width{}, 100, 0},
		{"percent", PercentWidth(30), 100, 30},
		{"percent rounds down", PercentWidth(30), 101, 30},
		{"percent of zero frame", PercentWidth(30), 0, 0},
		{"columns", ColumnsWidth(24), 100, 24},
		{"columns wider than the frame", ColumnsWidth(80), 40, 80},
		{"negative percent clamps to zero", PercentWidth(-5), 100, 0},
		{"percent above 100 clamps", PercentWidth(150), 100, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.width.Cols(tt.frame); got != tt.want {
				t.Errorf("%+v.Cols(%d) = %d, want %d", tt.width, tt.frame, got, tt.want)
			}
		})
	}
}

// TestWidthWithColsKeepsTheForm checks the grip-drag conversion: a column count
// stays exact, a percentage keeps its form by picking the nearer of the two
// bracketing percents (a tie goes to the wider), and an automatic width becomes
// a column count because there is no configured form to preserve.
func TestWidthWithColsKeepsTheForm(t *testing.T) {
	tests := []struct {
		name  string
		width Width
		cols  int
		frame int
		want  Width
	}{
		{"columns stay exact", ColumnsWidth(20), 25, 100, ColumnsWidth(25)},
		{"columns clamp to the frame", ColumnsWidth(20), 120, 100, ColumnsWidth(100)},
		{"a percent keeps a percent", PercentWidth(30), 40, 100, PercentWidth(40)},
		{"a percent is not exact on the frame", PercentWidth(30), 41, 100, PercentWidth(41)},
		{"a tie goes to the wider percent", PercentWidth(30), 50, 100, PercentWidth(50)},
		{"automatic becomes columns", Width{}, 24, 100, ColumnsWidth(24)},
		{"percent of a zero frame keeps its form", PercentWidth(30), 24, 0, PercentWidth(30)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.width.WithCols(tt.cols, tt.frame); got != tt.want {
				t.Errorf("%+v.WithCols(%d, %d) = %+v, want %+v", tt.width, tt.cols, tt.frame, got, tt.want)
			}
		})
	}
	// A percent drag lands on the columns it renders at.
	for _, pct := range []int{0, 1, 25, 30, 50, 99, 100} {
		frame := 137
		cols := PercentWidth(pct).Cols(frame)
		got := PercentWidth(pct).WithCols(cols, frame).Cols(frame)
		if got != cols {
			t.Errorf("percent %d at frame %d: round trip %d -> %d", pct, frame, cols, got)
		}
	}
}

// TestSide checks the config side names, the default and the toggle.
func TestSide(t *testing.T) {
	for _, tt := range []struct {
		name string
		side Side
		ok   bool
	}{
		{"left", SideLeft, true},
		{"right", SideRight, true},
		{"middle", SideLeft, false},
		{"", SideLeft, false},
	} {
		got, ok := ParseSide(tt.name)
		if ok != tt.ok || (ok && got != tt.side) {
			t.Errorf("ParseSide(%q) = %v, %v; want %v, %v", tt.name, got, ok, tt.side, tt.ok)
		}
	}
	if SideLeft.String() != "left" || SideRight.String() != "right" {
		t.Errorf("side names = %q, %q", SideLeft.String(), SideRight.String())
	}
	if SideLeft.Other() != SideRight || SideRight.Other() != SideLeft {
		t.Error("Other does not toggle the side")
	}
}

// TestDefaultOptions checks Fresh's non-width defaults: compact directories on,
// auto-open on the last buffer close on, following off, left side, and no width
// (the frame's automatic width applies when no config supplies one).
func TestDefaultOptions(t *testing.T) {
	got := DefaultOptions()
	want := Options{Side: SideLeft, CompactDirectories: true, AutoOpenOnLastBufferClose: true}
	if got != want {
		t.Errorf("DefaultOptions() = %+v, want %+v", got, want)
	}
}
