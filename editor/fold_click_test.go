package editor

import (
	"slices"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

func TestToggleFoldAtLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name       string
		caret      buffer.Pos
		Line       int
		toggles    int
		wantDid    bool
		wantFolded []int
		wantCaret  buffer.Pos
	}{
		{"collapses the fold and keeps the caret", caretPos(3, 0), 0, 1, true, []int{0}, caretPos(3, 0)},
		{"a second toggle expands it", caretPos(3, 0), 0, 2, true, nil, caretPos(3, 0)},
		{"a line that starts no fold changes nothing", caretPos(3, 0), 1, 1, false, nil, caretPos(3, 0)},
		{"a caret inside the body moves to the fold start", caretPos(1, 2), 0, 1, true, []int{0}, caretPos(0, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			e, _ := openFixture(t, Options{}, foldText)
			e.SetCursor(tt.caret, false)
			did := false
			for range tt.toggles {
				did = e.ToggleFoldAt(tt.Line)
			}
			if did != tt.wantDid {
				t.Fatalf("ToggleFoldAt reported %v, want %v", did, tt.wantDid)
			}
			d := e.Active()
			if got := foldedStarts(d, 4); !slices.Equal(got, tt.wantFolded) {
				t.Fatalf("folded = %v, want %v", got, tt.wantFolded)
			}
			if d.Cursor != tt.wantCaret {
				t.Fatalf("caret = %+v, want %+v", d.Cursor, tt.wantCaret)
			}
		})
	}
}
