package tui

import "testing"

// countingStash is an InterruptedSaves that counts its reads and lists one file.
type countingStash struct{ reads *int }

func (p countingStash) Interrupted() ([]Interrupted, error) {
	*p.reads++
	return []Interrupted{{Path: "/ws/a.txt", Stash: "/cache/stash-1.txt"}}, nil
}

// TestReviewInterruptedSavesReadsOnlyInItsCommand checks that the session read happens in
// the command the review returns, never in Update, and that its result opens the list.
func TestReviewInterruptedSavesReadsOnlyInItsCommand(t *testing.T) {
	t.Parallel()
	var reads int
	m, _ := newStashModel(t, countingStash{reads: &reads})
	cmd := rootPaletteRun(m, "review_interrupted_saves")
	if reads != 0 {
		t.Fatalf("the stash port was read %d times in Update, want 0", reads)
	}
	if cmd == nil {
		t.Fatal("the review returned no command")
	}
	msgs := collect(cmd)
	if reads != 1 {
		t.Fatalf("the stash port was read %d times in the command, want 1", reads)
	}
	for _, msg := range msgs {
		m.Update(msg)
	}
	if rows, ok := overlayRows(m); !ok || len(rows) != 1 {
		t.Fatalf("review did not open the list of one file: rows=%q ok=%v", rows, ok)
	}
}
