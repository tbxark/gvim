package prompt

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
)

func TestLongInputScrollsToTheCaret(t *testing.T) {
	t.Parallel()
	path := "/home/user/documents/projects/中文目录/reports/final-version-of-the-report.txt"
	for _, w := range []int{30, 50, 100} {
		l := NewLine(testStyles(), LineOptions{Label: "Save as:", Input: path})
		g := grid.New(w, 6)
		l.Draw(g)
		x, y, ok := l.Cursor(w, 6)
		if !ok {
			t.Fatalf("width %d: no caret", w)
		}
		left := strings.TrimRight(ansi.Truncate(g.Plain()[y], x, ""), " ")
		if !strings.HasSuffix(left, "report.txt") {
			t.Errorf("width %d: the text left of the caret is %q, want the end of the path", w, left)
		}
	}
}
