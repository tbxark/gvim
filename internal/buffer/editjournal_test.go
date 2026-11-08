package buffer

import (
	"math/rand/v2"
	"testing"
)

// EditedSince names the lines that edits touched, when they did not move any line, and every
// line outside them is as it was.
func TestEditedSinceBoundsTheDifferences(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(11, 3))
	b := New("one\ntwo\nthree\nfour\nfive\nsix\nseven")
	for round := range 200 {
		serial := b.EditSerial()
		before := make([]string, b.Lines())
		for i := range before {
			before[i] = b.Line(i)
		}
		edits := 1 + rng.IntN(5)
		for range edits {
			p := b.Clamp(Pos{Line: rng.IntN(b.Lines()), Col: rng.IntN(6)})
			switch rng.IntN(5) {
			case 0, 1:
				b.Insert(p, "z")
			case 2:
				b.Delete(p, b.Clamp(Pos{Line: p.Line, Col: p.Col + 2}))
			case 3:
				if rng.IntN(4) == 0 {
					b.Insert(p, "\nq")
				} else {
					b.Insert(p, "w")
				}
			case 4:
				b.Undo()
			}
		}
		lo, hi, ok := b.EditedSince(serial)
		if !ok {
			continue // an edit moved lines
		}
		if b.Lines() != len(before) {
			t.Fatalf("round %d: the line count changed from %d to %d but EditedSince answered", round, len(before), b.Lines())
		}
		for i := range before {
			if (i < lo || i > hi) && b.Line(i) != before[i] {
				t.Fatalf("round %d: line %d changed to %q from %q outside [%d, %d]", round, i, b.Line(i), before[i], lo, hi)
			}
		}
	}
}

func TestEditedSinceRefusesWhatItCannotAnswer(t *testing.T) {
	t.Parallel()
	b := New("a\nb\nc")
	start := b.EditSerial()
	b.Insert(Pos{Line: 1, Col: 1}, "x")
	if lo, hi, ok := b.EditedSince(start); !ok || lo != 1 || hi != 1 {
		t.Fatalf("one edit in a line: [%d, %d], %v; want [1, 1]", lo, hi, ok)
	}
	b.Insert(Pos{Line: 1, Col: 1}, "\n")
	if _, _, ok := b.EditedSince(start); ok {
		t.Error("an edit that added a line was answered")
	}
	if _, _, ok := b.EditedSince(b.EditSerial() + 1); ok {
		t.Error("a serial from the future was answered")
	}
	late := b.EditSerial()
	for range journalSize + 5 {
		b.Insert(Pos{}, "k")
	}
	if _, _, ok := b.EditedSince(late); ok {
		t.Error("a serial older than the journal was answered")
	}
	if lo, hi, ok := b.EditedSince(b.EditSerial()); !ok || lo <= hi {
		t.Errorf("no edits since now: [%d, %d], %v; want an empty answer", lo, hi, ok)
	}
}
