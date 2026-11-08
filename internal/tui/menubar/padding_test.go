package menubar

import (
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/space"
)

// TestDropdownRowsAreInsetOnBothSides checks that no text touches a border: every
// row has space.PadX blank cells between the left border and the label, and between
// the shortcut (or the label) and the right border. Separator rules are inset the
// same way.
func TestDropdownRowsAreInsetOnBothSides(t *testing.T) {
	t.Parallel()
	for _, title := range Labels(false) {
		m := newBar(allOn(), 100, 40).openTitle(title)
		geo := m.geometry(100, 40)
		if !geo.hasBox {
			continue
		}
		plain := paintRows(m)
		box := geo.box
		for k := range box.visible() {
			row := box.rows[box.first+k]
			cells := []rune(ansi.Cut(plain[box.top+1+k], box.x, box.x+box.w))
			n := len(cells)
			if cells[0] != '│' || cells[n-1] != '│' {
				t.Fatalf("%s row %q: %q is not framed by borders", title, row.Label, string(cells))
			}
			for p := 1; p <= space.PadX; p++ {
				left, right := cells[p], cells[n-1-p]
				if row.Separator {
					if left != ' ' || right != ' ' {
						t.Errorf("%s separator: edge cells %q %q, want padding", title, left, right)
					}
					continue
				}
				if left != ' ' || right != ' ' {
					t.Errorf("%s row %q: cells next to the borders are %q and %q, want padding", title, row.Label, left, right)
				}
			}
		}
	}
}

// TestDropdownWidthCountsThePadding checks that a row with the longest shortcut fits
// with padding on both sides.
func TestDropdownWidthCountsThePadding(t *testing.T) {
	t.Parallel()
	rows := []Row{{Label: "New File", Keys: "Ctrl+N"}}
	if got, want := boxWidth(rows), len("New File")+1+len("Ctrl+N")+2+2*space.PadX; got != want {
		t.Errorf("boxWidth = %d, want %d", got, want)
	}
}
