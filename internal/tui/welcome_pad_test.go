package tui

import (
	"testing"

	"github.com/tbxark/gvim/internal/tui/space"
)

// The rows of the welcome screen keep a cell of padding inside their highlight, and
// the block keeps a cell of margin from the pane edges, even when the pane is narrow
// and a long recent path would fill it.
func TestWelcomeRowsAreNotGluedToThePaneEdges(t *testing.T) {
	recents := []string{"/a/very/long/path/to/a/project/folder/that/does/not/fit/in/the/pane"}
	for _, paneW := range []int{30, 48, 60, 100} {
		x0 := 20
		lines := welcomeLines(x0, paneW, 0, 30, recents, nil)
		rows := 0
		for _, r := range lines {
			if r.item < 0 {
				continue
			}
			rows++
			// The highlight starts a cell inside the pane and ends a cell before its edge.
			if r.x < x0+space.PadX || r.x+r.w > x0+paneW-space.PadX {
				t.Errorf("pane %d: row %q highlight spans %d..%d, want a %d cell margin in %d..%d",
					paneW, r.text, r.x, r.x+r.w, space.PadX, x0, x0+paneW)
			}
			// The label starts inside the highlight, after its padding.
			if r.labelX < r.x+space.PadX {
				t.Errorf("pane %d: row %q label at %d is inside the highlight padding (starts %d)", paneW, r.text, r.labelX, r.x)
			}
			if r.keyX >= 0 && r.keyX+len([]rune(r.key)) > r.x+r.w-space.PadX {
				t.Errorf("pane %d: row %q key ends at %d, inside the highlight padding (ends %d)",
					paneW, r.text, r.keyX+len([]rune(r.key)), r.x+r.w)
			}
		}
		if rows == 0 {
			t.Fatalf("pane %d: no rows", paneW)
		}
	}
}
