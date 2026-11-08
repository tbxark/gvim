package find

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The options row must never cut a label in the middle: at every width each option is
// either whole, or the row ends with an ellipsis that marks the cut.
func TestOptionsRowFitsEveryWidth(t *testing.T) {
	t.Parallel()
	b := Open(newFake("x"), testStyles(), false)
	const h = 8
	opt := rowsFor(h, false).opt
	for w := 10; w <= 200; w++ {
		row := strings.TrimRight(frame(b, w, h)[opt], " ")
		if ansi.StringWidth(row) > w {
			t.Errorf("width %d: row %q is wider than the frame", w, row)
		}
		if strings.HasSuffix(row, "…") {
			continue
		}
		// Not cut: all four names are on the row, each whole.
		for _, name := range []string{"Case", "Word", "Regex", "Confirm"} {
			if !strings.Contains(row, name) {
				t.Errorf("width %d: row %q is not cut but lacks %q", w, row, name)
			}
		}
	}
}

// Each width of the row shows all four options when it can, and drops the key hints
// before it shortens the names.
func TestOptionsRowChoosesTheLabelForm(t *testing.T) {
	t.Parallel()
	b := Open(newFake("x"), testStyles(), false)
	cases := []struct {
		w    int
		want string
	}{
		{120, "[ ] Confirm Each (Alt+I)"},
		{80, "[ ] Confirm Each"},
		{60, "[ ] Confirm"},
	}
	for _, c := range cases {
		row := frame(b, c.w, 8)[rowsFor(8, false).opt]
		if !strings.Contains(row, c.want) {
			t.Errorf("width %d: row %q does not show %q", c.w, row, c.want)
		}
		if strings.Contains(row, "…") {
			t.Errorf("width %d: row %q is cut, want all four options", c.w, row)
		}
	}
}

// A click reaches the label that Draw painted in that column, at every width.
func TestOptionAtFollowsTheRowLayout(t *testing.T) {
	t.Parallel()
	b := Open(newFake("x"), testStyles(), false)
	for _, w := range []int{10, 20, 40, 60, 80, 120, 200} {
		labels, gap := b.optionRow(w)
		at := 1
		for i, label := range labels {
			lw := ansi.StringWidth(label)
			for x := at; x < at+lw; x++ {
				if got, ok := b.optionAt(x, w); !ok || got != i {
					t.Errorf("width %d: column %d is on option %d, optionAt says %d (%v)", w, x, i, got, ok)
				}
			}
			if gap > 0 {
				if _, ok := b.optionAt(at+lw, w); ok {
					t.Errorf("width %d: the blank after option %d reached an option", w, i)
				}
			}
			at += lw + gap
		}
	}
}
