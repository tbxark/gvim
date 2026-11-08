package find

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
)

const longQuery = "this is a very long query with 中文字符 that overflows the find bar width for sure"

func TestLongQueryScrollsToTheCaret(t *testing.T) {
	t.Parallel()
	for _, w := range []int{40, 60, 100} {
		b := Open(newFake("x"), testStyles(), false)
		typeText(b, longQuery)
		g := grid.New(w, 12)
		b.Draw(g)
		x, y, ok := b.Cursor(w, 12)
		if !ok {
			t.Fatalf("width %d: no caret", w)
		}
		row := g.Plain()[y]
		// The cells left of the caret hold the last typed runes.
		left := ansi.Truncate(row, x, "")
		if !strings.HasSuffix(strings.TrimRight(left, " "), "for sure") {
			t.Errorf("width %d: row %q, the text left of caret column %d is %q, want the end of the query", w, row, x, left)
		}
		if ansi.StringWidth(row) > w {
			t.Errorf("width %d: row is %d cells wide", w, ansi.StringWidth(row))
		}
	}
}

func TestLongReplacementScrollsToTheCaret(t *testing.T) {
	t.Parallel()
	b := Open(newFake("x"), testStyles(), true)
	press(b, "tab")
	typeText(b, longQuery)
	g := grid.New(50, 12)
	b.Draw(g)
	x, y, ok := b.Cursor(50, 12)
	if !ok {
		t.Fatal("no caret in the replacement row")
	}
	if left := strings.TrimRight(ansi.Truncate(g.Plain()[y], x, ""), " "); !strings.HasSuffix(left, "for sure") {
		t.Errorf("the text left of the caret is %q, want the end of the replacement", left)
	}
}

func TestSearchAndReplaceInputsStartInTheSameColumn(t *testing.T) {
	t.Parallel()
	b := Open(newFake("x"), testStyles(), true)
	typeText(b, "q")
	sx, _, _ := b.Cursor(80, 12)
	press(b, "tab")
	typeText(b, "r")
	rx, _, _ := b.Cursor(80, 12)
	if sx != rx {
		t.Errorf("search caret at column %d and replacement caret at %d, want the inputs aligned", sx, rx)
	}
	rows := frame(b, 80, 12)
	search, repl := rows[len(rows)-2], rows[len(rows)-1]
	if strings.Index(search, "q") != strings.Index(repl, "r") {
		t.Errorf("inputs are not aligned:\n%q\n%q", search, repl)
	}
}

func TestStatusIsDroppedWhenItWouldSqueezeTheInput(t *testing.T) {
	t.Parallel()
	b := Open(newFake("foo foo"), testStyles(), false)
	typeText(b, "foo")
	if !strings.Contains(frame(b, 60, 12)[11], "Match") {
		t.Fatalf("setup: no status at 60 columns: %q", frame(b, 60, 12)[11])
	}
	rows := frame(b, 26, 12)
	if strings.Contains(rows[11], "Match") {
		t.Errorf("the status squeezes the input at 26 columns: %q", rows[11])
	}
	if x, _, ok := b.Cursor(26, 12); !ok || x >= 26 {
		t.Errorf("Cursor = %d, %v, want a caret inside the row", x, ok)
	}
}
