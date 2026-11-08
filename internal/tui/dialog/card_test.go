package dialog

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/space"
)

// TestDialogCardRows pins the shared dialog rows: the bare title on the top border,
// the body directly under it, one blank row, the buttons on the last row inside the
// border, and the rounded bottom border.
func TestDialogCardRows(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	d := NewUnsavedChanges(c.Now, "a.txt", nil, testOv)
	g := draw(d, 80, 24)
	box := d.box
	rows := g.Plain()
	top := rows[box.Y]
	if !strings.Contains(top, "╭─ Unsaved Changes ─") {
		t.Fatalf("top border = %q, want the bare title on it", top)
	}
	if got := rows[box.Y+1]; !strings.Contains(got, "Save changes to a.txt?") {
		t.Fatalf("row %d = %q, want the body directly under the top border", box.Y+1, got)
	}
	gap := strings.TrimSpace(rows[box.Y+2])
	if strings.Contains(gap, "Save") || strings.Contains(gap, "Discard") || strings.Contains(gap, "Cancel") {
		t.Fatalf("row %d = %q, want a blank row between the body and the buttons", box.Y+2, gap)
	}
	buttonY := box.Y + box.H - 2
	if buttonY != box.Y+2+1 {
		t.Fatalf("button row = %d, want the last row inside the border", buttonY)
	}
	buttonRow := rows[buttonY]
	for _, label := range []string{"Save", "Discard", "Cancel"} {
		if !strings.Contains(buttonRow, label) {
			t.Fatalf("button row = %q, want %q on it", buttonRow, label)
		}
	}
	if _, y := mustFind(t, g, "  Discard  "); y != buttonY {
		t.Fatalf("the Discard button is on row %d, want %d", y, buttonY)
	}
	bottom := strings.TrimSpace(rows[box.Y+box.H-1])
	if !strings.HasPrefix(bottom, "╰") || !strings.HasSuffix(bottom, "╯") {
		t.Fatalf("bottom border = %q, want rounded corners", bottom)
	}
	if strings.Contains(bottom, "Save") {
		t.Fatalf("bottom border = %q, the buttons are not on the last row inside it", bottom)
	}
}

// TestDialogBodyStartsUnderTheTopBorder covers a dialog whose body is a text field
// and one whose body wraps to a message: neither leaves a padding row above it.
func TestDialogBodyStartsUnderTheTopBorder(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	for _, d := range []Overlay{
		NewSaveAs(c.Now, "a.txt", nil, PathChecks{}, testOv),
		NewErrorDialog(c.Now, "boom", nil, testOv),
		NewOpenFolder(c.Now, []string{"/recent"}, "/src", nil, nil, testOv),
	} {
		g := draw(d, 80, 24)
		var box rect
		switch v := d.(type) {
		case *SaveAs:
			box = v.box
		case *ErrorDialog:
			box = v.box
		case *OpenFolder:
			box = v.box
		}
		rows := g.Plain()
		if got := rows[box.Y+1]; strings.TrimSpace(got) == "" {
			t.Fatalf("%T: row %d is blank, want the body under the top border", d, box.Y+1)
		}
	}
}

// TestDialogCardKeepsTheMarginFromTheFrameEdge: the centred card keeps space.Margin
// from the left and right edge and at least one row from the top and bottom.
func TestDialogCardKeepsTheMarginFromTheFrameEdge(t *testing.T) {
	t.Parallel()
	c := newFakeClock()
	for _, w := range []int{80, 140} {
		d := NewUnsavedChanges(c.Now, "a.txt", nil, testOv)
		draw(d, w, 24)
		m := space.Margin(w)
		box := d.box
		if box.X < m || w-(box.X+box.W) < m {
			t.Errorf("width %d: box at %d..%d, want a margin of at least %d", w, box.X, box.X+box.W, m)
		}
		if box.Y < 1 || box.Y+box.H > 23 {
			t.Errorf("width %d: box rows %d..%d, want one row from the top and bottom", w, box.Y, box.Y+box.H)
		}
	}
}
