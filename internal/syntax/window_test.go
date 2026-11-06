package syntax

import (
	"slices"
	"testing"

	"github.com/alecthomas/chroma/v2/lexers"
)

func TestWindowLine(t *testing.T) {
	w := Window{First: 10, Spans: [][]Span{{{Start: 0, End: 1}}, nil, {{Start: 0, End: 2}}}}
	tests := []struct {
		line int
		want []Span
	}{
		{9, nil},
		{10, []Span{{Start: 0, End: 1}}},
		{11, nil},
		{12, []Span{{Start: 0, End: 2}}},
		{13, nil},
	}
	for _, tt := range tests {
		if got := w.Line(tt.line); !slices.Equal(got, tt.want) {
			t.Errorf("Line(%d) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

// TestLinesWindowScrollByOneMapsRightLines checks that two windows of the same
// version and length that differ in First hold the spans of their own text.
func TestLinesWindowScrollByOneMapsRightLines(t *testing.T) {
	h := NewHighlighter(lexers.Get("go"))
	lines := []string{"package main", "// note", "func f() {}", "var x = 1", "return"}
	a := h.LinesWindow(3, 0, lines[:3])
	b := h.LinesWindow(3, 1, lines[1:4])
	if a.First != 0 || b.First != 1 {
		t.Fatalf("First = %d and %d, want 0 and 1", a.First, b.First)
	}
	// Lines 1 and 2 are in both windows and carry the same spans.
	for _, i := range []int{1, 2} {
		if !slices.Equal(a.Line(i), b.Line(i)) {
			t.Errorf("line %d: %v in the first window, %v in the second", i, a.Line(i), b.Line(i))
		}
	}
	if a.Line(3) != nil {
		t.Errorf("line 3 is outside the first window and has spans %v", a.Line(3))
	}
	if got := b.Line(3); len(got) == 0 || got[len(got)-1].End != len(lines[3]) {
		t.Errorf("line 3 spans %v do not cover %q", got, lines[3])
	}
	if got := a.Line(0); len(got) == 0 || got[0].Kind != KindKeyword {
		t.Errorf("line 0 spans = %v, want a keyword first", got)
	}
}

func TestLinesWindowCachePerWindowKey(t *testing.T) {
	calls := 0
	h := NewHighlighter(countingLexer{Lexer: lexers.Get("python"), calls: &calls})
	text := []string{"def f():", "    return 1"}
	h.LinesWindow(1, 0, text)
	h.LinesWindow(1, 0, text)
	if calls != 1 {
		t.Fatalf("same window: lexed %d times, want 1", calls)
	}
	h.LinesWindow(1, 5, text) // another pane, another top
	if calls != 2 {
		t.Fatalf("other First: lexed %d times, want 2", calls)
	}
	h.LinesWindow(1, 0, text)
	h.LinesWindow(1, 5, text)
	if calls != 2 {
		t.Fatalf("both panes again: lexed %d times, want 2 (one entry per pane)", calls)
	}
	h.LinesWindow(2, 0, text)
	if calls != 3 {
		t.Fatalf("new version: lexed %d times, want 3", calls)
	}
}

func TestLinesWindowKeepsTheNewestFour(t *testing.T) {
	calls := 0
	h := NewHighlighter(countingLexer{Lexer: lexers.Get("python"), calls: &calls})
	text := []string{"x = 1"}
	for first := range 5 {
		h.LinesWindow(1, first, text)
	}
	if calls != 5 {
		t.Fatalf("lexed %d times, want 5", calls)
	}
	h.LinesWindow(1, 4, text) // the newest is kept
	if calls != 5 {
		t.Fatalf("newest window was lexed again (%d calls)", calls)
	}
	h.LinesWindow(1, 0, text) // the oldest was dropped
	if calls != 6 {
		t.Fatalf("oldest window was kept (%d calls), want it dropped", calls)
	}
}

func TestLinesWindowOversizeIsPlain(t *testing.T) {
	calls := 0
	h := NewHighlighter(countingLexer{Lexer: lexers.Get("go"), calls: &calls})
	text := bigLines(3, 1<<20)
	w := h.LinesWindow(1, 100, text)
	if calls != 0 || !h.Skipped() {
		t.Fatalf("oversize window: %d lexer calls, skipped %v", calls, h.Skipped())
	}
	for i := range text {
		if got := w.Line(100 + i); len(got) != 1 || got[0].Kind != KindPlain {
			t.Fatalf("line %d: %v, want one plain span", 100+i, got)
		}
	}
}

func TestInvalidateDropsWindows(t *testing.T) {
	calls := 0
	h := NewHighlighter(countingLexer{Lexer: lexers.Get("python"), calls: &calls})
	text := []string{"x = 1"}
	h.LinesWindow(1, 0, text)
	h.Invalidate()
	h.LinesWindow(1, 0, text)
	if calls != 2 {
		t.Fatalf("lexed %d times, want 2 after Invalidate", calls)
	}
}
