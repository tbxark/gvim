package search

import (
	"slices"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

// navMatches returns the three matches of "a" in "a x a x a".
func navMatches(t *testing.T) []Match {
	t.Helper()
	got, err := Find(buffer.New("a x a x a"), Query{Text: "a", CaseSensitive: true})
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Find() returned %d matches, want 3", len(got))
	}
	return got
}

func TestNext(t *testing.T) {
	t.Parallel()
	matches := navMatches(t)
	tests := []struct {
		name    string
		matches []Match
		from    buffer.Pos
		wrap    bool
		wantIdx int
		wantOK  bool
	}{
		{"at first match", matches, buffer.Pos{Line: 0, Col: 0}, false, 0, true},
		{"between matches", matches, buffer.Pos{Line: 0, Col: 1}, false, 1, true},
		{"after last, no wrap", matches, buffer.Pos{Line: 0, Col: 9}, false, -1, false},
		{"after last, wrap", matches, buffer.Pos{Line: 0, Col: 9}, true, 0, true},
		{"no matches", nil, buffer.Pos{}, true, -1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			idx, ok := Next(tt.matches, tt.from, tt.wrap)
			if idx != tt.wantIdx || ok != tt.wantOK {
				t.Fatalf("Next() = (%d, %v), want (%d, %v)", idx, ok, tt.wantIdx, tt.wantOK)
			}
		})
	}
}

func TestPrev(t *testing.T) {
	t.Parallel()
	matches := navMatches(t)
	tests := []struct {
		name    string
		matches []Match
		from    buffer.Pos
		wrap    bool
		wantIdx int
		wantOK  bool
	}{
		{"before first, no wrap", matches, buffer.Pos{Line: 0, Col: 0}, false, -1, false},
		{"before first, wrap", matches, buffer.Pos{Line: 0, Col: 0}, true, 2, true},
		{"at second match", matches, buffer.Pos{Line: 0, Col: 4}, false, 0, true},
		{"after last", matches, buffer.Pos{Line: 0, Col: 9}, false, 2, true},
		{"no matches", nil, buffer.Pos{}, true, -1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			idx, ok := Prev(tt.matches, tt.from, tt.wrap)
			if idx != tt.wantIdx || ok != tt.wantOK {
				t.Fatalf("Prev() = (%d, %v), want (%d, %v)", idx, ok, tt.wantIdx, tt.wantOK)
			}
		})
	}
}

// TestWrapAroundCycle presses next and then prev with wrap on, and checks that
// the walk visits every match in order and returns to where it started.
func TestWrapAroundCycle(t *testing.T) {
	t.Parallel()
	matches := navMatches(t)

	var forward []int
	pos := buffer.Pos{Line: 0, Col: 0}
	for range len(matches) + 1 {
		idx, ok := Next(matches, pos, true)
		if !ok {
			t.Fatalf("Next(%v) reported no match with wrap on", pos)
		}
		forward = append(forward, idx)
		pos = matches[idx].End
	}
	if want := []int{0, 1, 2, 0}; !slices.Equal(forward, want) {
		t.Fatalf("forward walk = %v, want %v", forward, want)
	}

	var backward []int
	pos = matches[0].Start
	for range len(matches) + 1 {
		idx, ok := Prev(matches, pos, true)
		if !ok {
			t.Fatalf("Prev(%v) reported no match with wrap on", pos)
		}
		backward = append(backward, idx)
		pos = matches[idx].Start
	}
	if want := []int{2, 1, 0, 2}; !slices.Equal(backward, want) {
		t.Fatalf("backward walk = %v, want %v", backward, want)
	}
}

func TestStatusText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		index, total int
		want         string
	}{
		{0, 3, "Match 1 of 3"},
		{2, 3, "Match 3 of 3"},
		{0, 0, "No matches"},
		{-1, 3, "3 matches"},
		{5, 3, "3 matches"},
	}
	for _, tt := range tests {
		if got := StatusText(tt.index, tt.total); got != tt.want {
			t.Errorf("StatusText(%d, %d) = %q, want %q", tt.index, tt.total, got, tt.want)
		}
	}
}
