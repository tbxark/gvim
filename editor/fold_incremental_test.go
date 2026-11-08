package editor

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/fold"
)

// After any run of edits, undo and redo, Folds gives the ranges of all the lines, though it reads
// again only the lines that edits touched when it can.
func TestFoldsFollowEveryEditWithoutReadingAllLines(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(5, 7))
	e, _ := openFixture(t, Options{}, strings.Repeat("a:\n  b\n    c\n  d\ne\n\n", 30))
	d := e.Active()
	scratch := func(tab int) []fold.Range {
		lines := strings.Split(d.Buffer.Text(), "\n")
		return fold.Ranges(lines, tab)
	}
	for step := range 300 {
		p := d.Buffer.Clamp(buffer.Pos{Line: rng.IntN(d.Buffer.Lines()), Col: rng.IntN(8)})
		switch rng.IntN(8) {
		case 0, 1, 2:
			d.Buffer.Insert(p, "x")
		case 3:
			d.Buffer.Insert(p, "  ")
		case 4:
			d.Buffer.Insert(buffer.Pos{Line: p.Line}, "  ")
		case 5:
			d.Buffer.Delete(p, d.Buffer.Clamp(buffer.Pos{Line: p.Line, Col: p.Col + 2}))
		case 6:
			d.Buffer.Insert(p, "\n  y")
		case 7:
			if rng.IntN(2) == 0 {
				d.Buffer.Undo()
			} else {
				d.Buffer.Redo()
			}
		}
		for _, tab := range []int{4, 2} {
			if got, want := d.Folds(tab), scratch(tab); !slices.Equal(got, want) {
				t.Fatalf("step %d, tab %d: Folds = %v, want %v", step, tab, got, want)
			}
		}
	}
}

// An edit that keeps the indentation of its line leaves the ranges as they were: the same slice
// comes back, so nothing was found again.
func TestFoldsAreNotFoundAgainForTypingInALine(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{}, "a\n  b\n  c\nd\n")
	d := e.Active()
	first := d.Folds(4)
	d.Buffer.Insert(buffer.Pos{Line: 1, Col: 3}, "text")
	if again := d.Folds(4); len(again) == 0 || &again[0] != &first[0] {
		t.Error("typing in a line found the ranges again")
	}
	d.Buffer.Insert(buffer.Pos{Line: 1}, "  ")
	if got := d.Folds(4); len(got) == 0 || &got[0] == &first[0] {
		t.Errorf("an edit of the indentation did not find the ranges again: %v", got)
	}
}
