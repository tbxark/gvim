package editorpane

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

func TestHorizontalMarginKeepsColumnsAroundTheCaret(t *testing.T) {
	long := strings.Repeat("0123456789", 10)
	// A 40-column pane with the gutter 6 has 33 text columns and a margin of 5.
	tests := []struct {
		name     string
		caret    int
		left     int
		keepLeft bool
		want     int
	}{
		{"caret inside the margins keeps the window", 10, 0, false, 0},
		{"right margin scrolls early", 30, 0, false, 30 - 33 + 1 + 5},
		{"left margin scrolls back early", 33, 30, false, 28},
		{"the window never starts before column 0", 2, 3, false, 0},
		{"the end of the line needs no margin", 100, 0, false, 100 - 33 + 1},
		{"a panned view stays off the caret", 0, 30, true, 30},
		{"without the pan the caret pulls the view back", 0, 30, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDoc(long)
			d.cursor = buffer.Pos{Col: tt.caret}
			left, _ := Pane{Doc: d, Left: tt.left, KeepLeft: tt.keepLeft}.HScroll(40, 5)
			if left != tt.want {
				t.Fatalf("left = %d, want %d", left, tt.want)
			}
		})
	}
}

func TestHScrollReportsTheLargestFirstColumn(t *testing.T) {
	d := newDoc(strings.Repeat("x", 100))
	if _, most := (Pane{Doc: d}).HScroll(40, 5); most != 100-33 {
		t.Errorf("most = %d, want %d", most, 100-33)
	}
	if _, most := (Pane{Doc: d, Wrap: true}).HScroll(40, 5); most != 0 {
		t.Errorf("most with Wrap on = %d, want 0", most)
	}
	if _, most := (Pane{Doc: newDoc("short")}).HScroll(40, 5); most != 0 {
		t.Errorf("most for a short line = %d, want 0", most)
	}
}

func TestGutterHitMarksTheFoldToggle(t *testing.T) {
	tests := []struct {
		name   string
		folded map[int]bool
		hide   bool
		y      int
		x      int
		want   bool
	}{
		{"marker cell of a fold start", nil, false, 0, 5, true},
		{"line number of a fold start", nil, false, 0, 1, true},
		{"a body line is not a fold start", nil, false, 1, 5, false},
		{"markers hidden: an open fold is not toggled", nil, true, 0, 5, false},
		{"markers hidden: a collapsed fold is still toggled", map[int]bool{0: true}, true, 0, 5, true},
		{"a text cell is not the gutter", nil, false, 0, 8, false},
		{"a plain line", nil, false, 3, 5, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Pane{Doc: foldFixture(tt.folded), HideFoldMarkers: tt.hide}
			h := p.Hit(20, 5, tt.x, tt.y)
			if h.Fold != tt.want {
				t.Fatalf("Hit = %+v, want Fold %v", h, tt.want)
			}
		})
	}
}

func TestWrappedContinuationRowIsNotAFoldToggle(t *testing.T) {
	doc := foldFixture(nil)
	doc.fakeDoc = newDoc(strings.Repeat("a", 30)+" {", "\tx := 1", "\ty := 2")
	p := Pane{Doc: doc, Wrap: true}
	if h := p.Hit(20, 6, 5, 0); !h.Fold {
		t.Fatalf("first row Hit = %+v, want a fold toggle", h)
	}
	if h := p.Hit(20, 6, 5, 1); h.Fold || h.Pos.Line != 0 {
		t.Fatalf("continuation row Hit = %+v, want line 0 with no fold toggle", h)
	}
}
