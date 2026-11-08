package list

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/style"
)

// cardRows returns the plain rows of a list drawn in a w by h frame.
func cardRows(v *View, w, h int) []string {
	g := grid.New(w, h)
	v.draw(g)
	return g.Plain()
}

// TestListCardFrame pins the shared card frame: a rounded border, the title on the
// top border after one blank cell, with the plain rounded rule after it.
func TestListCardFrame(t *testing.T) {
	t.Parallel()
	v := New("T", nil, style.Overlay{}, nil)
	rows := cardRows(v, 80, 24)
	top := rows[v.Box.Y]
	if !strings.Contains(top, "╭─ T ─") {
		t.Fatalf("top border = %q, want a rounded corner, a bare title and the title fill", top)
	}
	if strings.Contains(top, "╭─  T") {
		t.Fatalf("top border = %q, the title has a double space", top)
	}
	bottom := strings.TrimSpace(rows[v.Box.Y+v.Box.H-1])
	if !strings.HasPrefix(bottom, "╰") || !strings.HasSuffix(bottom, "╯") {
		t.Fatalf("bottom border = %q, want rounded corners", bottom)
	}
}

// TestListFooterIsOnTheBottomBorder: the key hints are part of the bottom border
// row, not a row of their own above it.
func TestListFooterIsOnTheBottomBorder(t *testing.T) {
	t.Parallel()
	v := New("T", filterRows(), style.Overlay{}, nil)
	rows := cardRows(v, 80, 24)
	bottom := v.Box.Y + v.Box.H - 1
	if !strings.Contains(rows[bottom], "Esc close") {
		t.Fatalf("bottom border = %q, want the hints on it", rows[bottom])
	}
	if strings.Contains(rows[bottom-1], "Esc close") {
		t.Fatalf("row %d = %q, want no separate footer row", bottom-1, rows[bottom-1])
	}
}

// TestListCardKeepsTheMarginFromTheFrameEdge: the centred card keeps at least
// space.Margin from the left and right edge and one row from the top and bottom.
func TestListCardKeepsTheMarginFromTheFrameEdge(t *testing.T) {
	t.Parallel()
	for _, w := range []int{59, 80, 140} {
		v := New("T", nil, style.Overlay{}, nil)
		cardRows(v, w, 24)
		want := space.Margin(w)
		if v.Box.X < want || w-(v.Box.X+v.Box.W) < want {
			t.Errorf("width %d: box at %d..%d, want a margin of at least %d", w, v.Box.X, v.Box.X+v.Box.W, want)
		}
		if v.Box.Y < 1 || v.Box.Y+v.Box.H > 23 {
			t.Errorf("width %d: box rows %d..%d, want one row from the top and bottom", w, v.Box.Y, v.Box.Y+v.Box.H)
		}
	}
}

// TestTextBoxPutsTheBodyUnderTheTopBorderAndTheFooterOnTheBottomBorder pins the
// text box to the same frame as the list.
func TestTextBoxPutsTheBodyUnderTheTopBorderAndTheFooterOnTheBottomBorder(t *testing.T) {
	t.Parallel()
	v := NewTextBox(style.Overlay{}, func() (string, []string, string) {
		return "Tour", []string{"first", "second"}, "Esc close"
	})
	g := grid.New(80, 24)
	v.draw(g)
	rows := g.Plain()
	if got := rows[v.Box.Y]; !strings.Contains(got, "╭─ Tour ─") {
		t.Fatalf("top border = %q, want a bare title", got)
	}
	if got := rows[v.Box.Y+1]; !strings.Contains(got, "first") {
		t.Fatalf("row %d = %q, want the body directly under the top border", v.Box.Y+1, got)
	}
	bottom := strings.TrimSpace(rows[v.Box.Y+v.Box.H-1])
	if !strings.Contains(bottom, "Esc close") || !strings.HasPrefix(bottom, "╰") {
		t.Fatalf("bottom border = %q, want the footer on it", bottom)
	}
}
