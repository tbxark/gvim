package display

import (
	"slices"
	"testing"
)

func TestLineIndent(t *testing.T) {
	tests := []struct {
		line    string
		tab     int
		width   int
		content bool
	}{
		{"", 4, 0, false},
		{"    ", 4, 4, false},
		{"  \t", 4, 4, false},
		{"\tx", 4, 4, true},
		{"  x", 4, 2, true},
		{"\t\tx", 2, 4, true},
		{"  \r", 4, 2, false},
		{"x", 0, 0, true},
	}
	for _, tc := range tests {
		w, c := LineIndent(tc.line, tc.tab)
		if w != tc.width || c != tc.content {
			t.Errorf("LineIndent(%q, %d) = (%d, %v), want (%d, %v)", tc.line, tc.tab, w, c, tc.width, tc.content)
		}
	}
}

func TestGuideScannerFollowsTheFilesIndent(t *testing.T) {
	// A file indented by two, with a tab width of four.
	lines := []string{"if a {", "  b()", "  if c {", "    d()", "  }", "}"}
	want := [][]int{nil, {0}, {0}, {0, 2}, {0}, nil}
	s := NewGuideScanner(4, nil)
	for i, l := range lines {
		w, _ := LineIndent(l, 4)
		if got := s.Content(w); !slices.Equal(got, want[i]) {
			t.Errorf("line %d (%q): guides = %v, want %v", i, l, got, want[i])
		}
	}
}

func TestGuideScannerFallsBackToTabStopsAboveTheKnownBlocks(t *testing.T) {
	// The blocks that opened above the window are unknown, so a deep line gets tab stops.
	s := NewGuideScanner(4, nil)
	if got, want := s.Content(8), []int{0, 4}; !slices.Equal(got, want) {
		t.Errorf("guides = %v, want %v", got, want)
	}
}

func TestGuideScannerBlankLines(t *testing.T) {
	s := NewGuideScanner(4, nil)
	s.Content(0)
	s.Content(4)
	tests := []struct {
		name string
		next int
		want []int
	}{
		{"blank between lines of the same block", 4, []int{0}},
		{"blank before a deeper body", 8, []int{0, 4}},
		{"blank before a shallower line", 0, []int{0}},
		{"blank at the end", 0, []int{0}},
	}
	for _, tc := range tests {
		if got := s.Blank(tc.next); !slices.Equal(got, tc.want) {
			t.Errorf("%s: guides = %v, want %v", tc.name, got, tc.want)
		}
	}
	// A blank line opens no block: the staircase still ends at 4.
	if got, want := s.Content(8), []int{0, 4}; !slices.Equal(got, want) {
		t.Errorf("after blanks: guides = %v, want %v", got, want)
	}
}

func TestGuideScannerContinuationRowsReplayTheParentsGuides(t *testing.T) {
	s := NewGuideScanner(4, nil)
	s.Content(0)
	s.Content(2)
	s.Content(6)
	if got, want := s.Continuation(6), []int{0, 2}; !slices.Equal(got, want) {
		t.Errorf("padding of 6: guides = %v, want %v", got, want)
	}
	if got := s.Continuation(0); len(got) != 0 {
		t.Errorf("no padding: guides = %v, want none", got)
	}
}

func TestGuideAncestorsWalkUpToTheOpenBlocks(t *testing.T) {
	lines := []string{"func f() {", "\tif a {", "", "\t\tx", "\t}", "\t\ty"}
	line := func(i int) string { return lines[i] }
	// Above line 5 the open blocks are the func (0) and the if (4); the closing brace at 4 is
	// a content line at the if's own indent, so it is the nearest ancestor.
	if got, want := GuideAncestors(line, 5, 4, 200), []int{0, 4}; !slices.Equal(got, want) {
		t.Errorf("ancestors = %v, want %v", got, want)
	}
	if got := GuideAncestors(line, 0, 4, 200); len(got) != 0 {
		t.Errorf("ancestors of the first line = %v, want none", got)
	}
	if got, want := GuideAncestors(line, 5, 4, 1), []int{4}; !slices.Equal(got, want) {
		t.Errorf("ancestors with a one-line reach = %v, want %v", got, want)
	}
}
