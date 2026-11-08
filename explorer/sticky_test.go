package explorer

import (
	"reflect"
	"testing"
)

// stickyRows is a tree of 10 rows where rows 1-3 form one branch (root/a/b/file)
// and rows 4-9 are root children. Parent indices are the nearest row ancestor.
//
//	0 root
//	1   a        parent 0
//	2     b      parent 1
//	3       f1   parent 2
//	4   c1       parent 0
//	...
//	9   c6       parent 0
func stickyRows() []Row {
	rows := []Row{
		{Name: "root", Depth: 0, Parent: -1},
		{Name: "a", Depth: 1, Parent: 0},
		{Name: "b", Depth: 2, Parent: 1},
		{Name: "f1", Depth: 3, Parent: 2},
	}
	for i := range 6 {
		rows = append(rows, Row{Name: "c" + string(rune('1'+i)), Depth: 1, Parent: 0})
	}
	return rows
}

// TestStickyPinsAreTheRunFirstRowsAncestors checks the pinned band: the Parent
// chain of the run's first row, outermost first, capped at MaxStickyAncestors
// and at bodyRows-1 so the run keeps a row.
func TestStickyPinsAreTheRunFirstRowsAncestors(t *testing.T) {
	rows := stickyRows()
	tests := []struct {
		name     string
		offset   int
		bodyRows int
		want     []int
	}{
		{name: "the root pins nothing", offset: 0, bodyRows: 5, want: nil},
		{name: "a root child pins the root", offset: 4, bodyRows: 5, want: []int{0}},
		{name: "a depth-2 row pins root and a", offset: 2, bodyRows: 5, want: []int{0, 1}},
		{name: "a depth-3 row pins the whole chain", offset: 3, bodyRows: 5, want: []int{0, 1, 2}},
		{name: "the band leaves one row for the run", offset: 3, bodyRows: 2, want: []int{2}},
		{name: "the band is capped at MaxStickyAncestors", offset: 3, bodyRows: MaxStickyAncestors + 2, want: []int{0, 1, 2}},
		{name: "an offset past the rows pins nothing", offset: 99, bodyRows: 5, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StickyPins(rows, tt.offset, tt.bodyRows); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("StickyPins(offset %d, body %d) = %v, want %v", tt.offset, tt.bodyRows, got, tt.want)
			}
		})
	}
}

// TestMaxScrollLeavesTheLastRowVisible checks Fresh's ceiling: the largest offset
// whose run, under its own pins, still reaches the last row.
func TestMaxScrollLeavesTheLastRowVisible(t *testing.T) {
	rows := stickyRows()
	tests := []struct {
		bodyRows int
		want     int
	}{
		{bodyRows: 0, want: 0},
		{bodyRows: 4, want: 7},  // 3 run rows under the root pin: rows 7..9
		{bodyRows: 10, want: 0}, // the whole tree fits
		{bodyRows: 20, want: 0},
	}
	for _, tt := range tests {
		if got := MaxScroll(rows, tt.bodyRows); got != tt.want {
			t.Errorf("MaxScroll(body %d) = %d, want %d", tt.bodyRows, got, tt.want)
		}
	}
	if got := MaxScroll(nil, 4); got != 0 {
		t.Errorf("MaxScroll(no rows) = %d, want 0", got)
	}
}

// TestViewportClampsAndPins checks the resolved view: the raw offset is clamped
// to the ceiling and the pins come from the clamped offset.
func TestViewportClampsAndPins(t *testing.T) {
	rows := stickyRows()
	off, pins := Viewport(rows, 99, 4)
	if off != 7 || !reflect.DeepEqual(pins, []int{0}) {
		t.Fatalf("Viewport(99, 4) = off %d pins %v, want 7 and [0]", off, pins)
	}
	off, pins = Viewport(rows, 3, 6)
	if off != 3 || !reflect.DeepEqual(pins, []int{0, 1, 2}) {
		t.Fatalf("Viewport(3, 6) = off %d pins %v, want 3 and [0 1 2]", off, pins)
	}
}

// TestFollowScrollBringsTheRowUnderThePins checks what the selection asks for:
// the offset whose run holds the row, with the pinned band accounted for, and
// never past the ceiling.
func TestFollowScrollBringsTheRowUnderThePins(t *testing.T) {
	rows := stickyRows()
	tests := []struct {
		name     string
		index    int
		offset   int
		bodyRows int
		want     int
	}{
		{name: "the row is already in the run", index: 5, offset: 4, bodyRows: 4, want: 4},
		{name: "a row below the run scrolls it in", index: 3, offset: 0, bodyRows: 3, want: 3},
		{name: "a row above the run scrolls up", index: 2, offset: 7, bodyRows: 4, want: 2},
		{name: "the ceiling holds", index: 9, offset: 0, bodyRows: 4, want: 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FollowScroll(rows, tt.index, tt.offset, tt.bodyRows)
			run := tt.bodyRows - len(StickyPins(rows, got, tt.bodyRows))
			if tt.index < got || tt.index >= got+run {
				t.Fatalf("FollowScroll(index %d, offset %d, body %d) = %d: row not in the run %d..%d", tt.index, tt.offset, tt.bodyRows, got, got, got+run-1)
			}
			if got != tt.want {
				t.Errorf("FollowScroll(index %d, offset %d, body %d) = %d, want %d", tt.index, tt.offset, tt.bodyRows, got, tt.want)
			}
		})
	}
}
