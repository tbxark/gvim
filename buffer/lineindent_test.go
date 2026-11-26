package buffer

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/textlayout"
)

// TestLineIndent checks the indentation and blank flag against the string functions
// the fold ranges were found with: textlayout.LineIndent and strings.TrimSpace.
func TestLineIndent(t *testing.T) {
	t.Parallel()
	lines := []string{
		"", "   ", "\t", "\t  ", "x", "  x", "\tx", " \t x", "\t\ty z", "  é", " x", "  x",
		"  　", "a  ", "\v\f", "  \t \r",
	}
	indentWidth := func(line string, tab int) int {
		w, _ := textlayout.LineIndent(line, tab)
		return w
	}
	for _, tab := range []int{0, 1, 4, 8} {
		b := New(strings.Join(lines, "\n"))
		for i, line := range lines {
			w, blank := b.LineIndent(i, tab)
			wantBlank := strings.TrimSpace(line) == ""
			if blank != wantBlank || (!wantBlank && w != indentWidth(line, tab)) {
				t.Errorf("tab %d line %q: LineIndent = (%d, %v), want (%d, %v)",
					tab, line, w, blank, indentWidth(line, tab), wantBlank)
			}
		}
	}
	if w, blank := New("a\n  b").LineIndent(9, 4); w != 2 || blank {
		t.Errorf("an index past the end is clamped to the last line: got (%d, %v)", w, blank)
	}
}
