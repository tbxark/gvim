package buffer

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

// outside returns the lines of text that EditedLines promises are the saved ones, and the
// lines of saved they must equal: the lines before lo, and the lines from hi on.
func outside(t *testing.T, b *Buffer, saved []string) {
	t.Helper()
	lo, hi, ok := b.EditedLines()
	if !ok {
		t.Fatal("a memory buffer must report its edited lines")
	}
	n := b.Lines()
	if lo < 0 || hi < lo || hi > n {
		t.Fatalf("EditedLines = [%d, %d) for %d lines", lo, hi, n)
	}
	for i := range lo {
		if i >= len(saved) || b.Line(i) != saved[i] {
			t.Fatalf("line %d is before lo=%d but differs from the saved text: %q vs %v", i, lo, b.Line(i), saved)
		}
	}
	for i := hi; i < n; i++ {
		j := len(saved) - (n - i)
		if j < 0 || b.Line(i) != saved[j] {
			t.Fatalf("line %d is after hi=%d but differs from the saved line at the same distance from the end (%d of %d)", i, hi, j, len(saved))
		}
	}
}

func TestEditedLinesNarrowsTypingToOneLine(t *testing.T) {
	t.Parallel()
	b := New("a\nb\nc\nd\ne")
	if lo, hi, ok := b.EditedLines(); !ok || lo != hi {
		t.Fatalf("a new buffer has EditedLines = [%d, %d), %v; want an empty range", lo, hi, ok)
	}
	b.Insert(Pos{Line: 2, Col: 1}, "x")
	if lo, hi, _ := b.EditedLines(); lo != 2 || hi != 3 {
		t.Fatalf("after typing on line 2: [%d, %d), want [2, 3)", lo, hi)
	}
	b.Insert(Pos{Line: 2, Col: 2}, "y\nz")
	if lo, hi, _ := b.EditedLines(); lo != 2 || hi != 4 {
		t.Fatalf("after a line break on line 2: [%d, %d), want [2, 4)", lo, hi)
	}
	b.Delete(Pos{Line: 0, Col: 0}, Pos{Line: 1, Col: 0})
	if lo, hi, _ := b.EditedLines(); lo != 0 || hi != 3 {
		t.Fatalf("after joining lines 0 and 1: [%d, %d), want [0, 3)", lo, hi)
	}
	b.MarkSaved()
	if lo, hi, _ := b.EditedLines(); lo != hi {
		t.Fatalf("after MarkSaved: [%d, %d), want an empty range", lo, hi)
	}
}

func TestLargeBufferHasNoEditedLines(t *testing.T) {
	t.Parallel()
	var b Buffer
	b.big = &bigStore{}
	if _, _, ok := b.EditedLines(); ok {
		t.Fatal("a large buffer keeps no saved text, so it has no edited range")
	}
}

// Whatever the edits, the lines outside the range are the saved ones. The edits are random
// inserts and deletes, with undo and redo in between.
func TestEditedLinesBoundsEveryDifference(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(7, 11))
	for round := range 40 {
		saved := strings.Split(strings.Repeat("alpha\nbeta gamma\n\ndelta\n", 3)+"end", "\n")
		b := New(strings.Join(saved, "\n"))
		for step := range 60 {
			switch rng.IntN(7) {
			case 0, 1, 2:
				p := b.Clamp(Pos{Line: rng.IntN(b.Lines() + 1), Col: rng.IntN(12)})
				b.Insert(p, []string{"x", "yy", "\n", "a\nb", "\n\n", "é"}[rng.IntN(6)])
			case 3, 4:
				from := b.Clamp(Pos{Line: rng.IntN(b.Lines()), Col: rng.IntN(12)})
				to := b.Clamp(Pos{Line: from.Line + rng.IntN(3), Col: rng.IntN(12)})
				b.Delete(from, to)
			case 5:
				b.Undo()
			case 6:
				b.Redo()
			}
			outside(t, b, saved)
			if t.Failed() {
				t.Fatalf("round %d step %d", round, step)
			}
		}
		b.MarkSaved()
		saved = strings.Split(b.Text(), "\n")
		for range 20 {
			b.Insert(b.Clamp(Pos{Line: rng.IntN(b.Lines()), Col: rng.IntN(8)}), "q")
			outside(t, b, saved)
		}
	}
}

// Editing a line in place must leave undo and redo exact, including after typing at the
// end of a line and deleting back from it.
func TestInPlaceLineEditsKeepUndoExact(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(3, 5))
	for round := range 30 {
		b := New("one\ntwo three\nfour")
		model := strings.Split(b.Text(), "\n")
		var snapshots [][]string
		for range 50 {
			line := rng.IntN(len(model))
			runes := []rune(model[line])
			col := rng.IntN(len(runes) + 1)
			if rng.IntN(3) == 0 && len(runes) > 0 {
				// Delete up to the end of the line, or a span inside it.
				to := col + rng.IntN(len(runes)-col+1)
				if to == col {
					continue
				}
				snapshots = append(snapshots, slices.Clone(model))
				b.Delete(Pos{Line: line, Col: col}, Pos{Line: line, Col: to})
				model[line] = string(runes[:col]) + string(runes[to:])
			} else {
				snapshots = append(snapshots, slices.Clone(model))
				b.Insert(Pos{Line: line, Col: col}, "xy")
				model[line] = string(runes[:col]) + "xy" + string(runes[col:])
			}
			b.Break()
		}
		if got := b.Text(); got != strings.Join(model, "\n") {
			t.Fatalf("round %d: text %q, model %q", round, got, strings.Join(model, "\n"))
		}
		for k := len(snapshots) - 1; k >= 0; k-- {
			if _, ok := b.Undo(); !ok {
				t.Fatalf("round %d: undo %d refused", round, k)
			}
			if got, want := b.Text(), strings.Join(snapshots[k], "\n"); got != want {
				t.Fatalf("round %d: after undo %d text %q, want %q", round, k, got, want)
			}
		}
		for k := range snapshots {
			if _, ok := b.Redo(); !ok {
				t.Fatalf("round %d: redo %d refused", round, k)
			}
		}
		if got := b.Text(); got != strings.Join(model, "\n") {
			t.Fatalf("round %d: after redo text %q, want %q", round, got, strings.Join(model, "\n"))
		}
	}
}
