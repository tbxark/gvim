package find

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/buffer"
)

// optionX is the first column of option i on the options row.
func optionX(b *Bar, i int) int {
	x := 1
	for k := range i {
		x += ansi.StringWidth(optionLabel(k, b.opts[k])) + labelGap
	}
	return x
}

func TestClickOnAnOptionLabelTogglesIt(t *testing.T) {
	f := newFake("Foo foo")
	b := Open(f, testStyles(), false)
	typeText(b, "foo")
	const w, h = 120, 8
	opt := rowsFor(h, false).opt
	for i, name := range []string{"case", "word", "regex", "confirm"} {
		x := optionX(b, i)
		if !b.Click(x+2, opt, w, h) {
			t.Fatalf("%s: a click on the label was not taken", name)
		}
		if !b.opts[i] {
			t.Errorf("%s: option %d is still off after the click", name, i)
		}
		if row := frame(b, w, h)[opt]; !strings.Contains(row, "[x] "+optionLabels[i]) {
			t.Errorf("%s: options row %q does not show the checked box", name, row)
		}
		b.Click(x+2, opt, w, h)
		if b.opts[i] {
			t.Errorf("%s: a second click did not turn it off", name)
		}
	}
	if got := b.statusText(); !strings.Contains(got, "Confirm each disabled") {
		t.Errorf("status %q does not report the last toggle", got)
	}
}

func TestClickOnAnOptionSearchesAgain(t *testing.T) {
	b := Open(newFake("Foo foo"), testStyles(), false)
	typeText(b, "foo")
	if got := b.statusText(); got != "Match 1 of 2" {
		t.Fatalf("setup: status = %q", got)
	}
	b.Click(optionX(b, 0)+1, rowsFor(8, false).opt, 120, 8)
	b.Update(key("backspace"))
	typeText(b, "o")
	if got := b.statusText(); got != "Match 1 of 1" {
		t.Fatalf("status = %q after turning Case Sensitive on with the mouse", got)
	}
}

func TestClickBetweenLabelsAndOutsideTheBarIsNotAToggle(t *testing.T) {
	b := Open(newFake("foo"), testStyles(), false)
	const w, h = 120, 8
	opt := rowsFor(h, false).opt
	gap := optionX(b, 1) - 2 // a blank between the first two labels
	if !b.Click(gap, opt, w, h) {
		t.Error("a click on the options row between labels was not taken by the bar")
	}
	for i, on := range b.opts {
		if on {
			t.Errorf("option %d turned on by a click between labels", i)
		}
	}
	if b.Click(5, 0, w, h) || b.Click(5, opt-1, w, h) {
		t.Error("a click above the bar was taken")
	}
	if b.Click(w+3, opt, w, h) {
		t.Error("a click right of the frame was taken")
	}
}

func TestClickOnTheRowsMovesTheKeys(t *testing.T) {
	b := Open(newFake("foo"), testStyles(), true)
	const w, h = 120, 8
	r := rowsFor(h, true)
	if !b.Click(3, r.repl, w, h) || !b.inReplace {
		t.Fatalf("a click on the replacement row left the keys on the search row (inReplace %v)", b.inReplace)
	}
	typeText(b, "x")
	if b.repl.Value() != "x" || b.search.Value() != "" {
		t.Errorf("typed text went to search %q, replace %q", b.search.Value(), b.repl.Value())
	}
	if !b.Click(3, r.search, w, h) || b.inReplace {
		t.Fatal("a click on the search row did not move the keys back")
	}
}

func TestCoversListsTheRowsOfTheBar(t *testing.T) {
	for _, replace := range []bool{false, true} {
		b := Open(newFake("foo"), testStyles(), replace)
		const h = 10
		var covered []int
		for y := range h {
			if b.Covers(y, h) {
				covered = append(covered, y)
			}
		}
		if len(covered) != b.Rows() || covered[len(covered)-1] != h-1 {
			t.Errorf("replace %v: covers rows %v, want the last %d rows", replace, covered, b.Rows())
		}
	}
}

func TestRebaseKeepsEscFromJumpingBack(t *testing.T) {
	f := newFake("foo bar\nfoo baz\n")
	b := Open(f, testStyles(), false)
	typeText(b, "foo")
	b.Update(key("f3")) // the bar moves the selection to a match
	if !b.moved {
		t.Fatal("setup: the bar did not move the selection")
	}
	f.Place(buffer.Pos{Line: 1, Col: 5}) // a click in the text
	b.Rebase()
	b.Cancel()
	if f.cursor != (buffer.Pos{Line: 1, Col: 5}) {
		t.Errorf("Esc put the caret at %v, want it kept where the click put it", f.cursor)
	}
}
