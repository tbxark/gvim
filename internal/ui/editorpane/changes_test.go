package editorpane

import (
	"maps"
	"testing"
)

func TestChangeMarksForModifiedLine(t *testing.T) {
	bar, scroll := changeMarks([]string{"one", "two", "three"}, []string{"one", "TWO", "three"})
	if !maps.Equal(scroll, map[int]bool{1: true}) {
		t.Errorf("scroll = %v, want only line 1", scroll)
	}
	if !maps.Equal(bar, map[int]bool{1: true}) {
		t.Errorf("bar = %v, want only line 1", bar)
	}
}

func TestChangeMarksForInsertedLineMarkTheRowPastTheEnd(t *testing.T) {
	bar, scroll := changeMarks([]string{"a"}, []string{"a", "b"})
	if !maps.Equal(scroll, map[int]bool{1: true, 2: true}) {
		t.Errorf("scroll = %v, want lines 1 and the row past the end", scroll)
	}
	if !maps.Equal(bar, map[int]bool{1: true}) {
		t.Errorf("bar = %v, want the new line", bar)
	}
}

func TestChangeMarksForDeletedLineMarkOnlyTheTail(t *testing.T) {
	bar, scroll := changeMarks([]string{"a", "b", "c"}, []string{"a", "c"})
	// The deleted line only removed text, so no bar. Line 1 now holds "c", which
	// differs from the saved "b", and the row past the end is marked.
	if bar != nil {
		t.Errorf("bar = %v, want none for a pure deletion", bar)
	}
	if !maps.Equal(scroll, map[int]bool{1: true, 2: true}) {
		t.Errorf("scroll = %v, want lines 1 and 2", scroll)
	}
}

func TestChangeMarksNilWhenNothingDiffers(t *testing.T) {
	bar, scroll := changeMarks([]string{"a", "b"}, []string{"a", "b"})
	if bar != nil || scroll != nil {
		t.Errorf("marks = %v, %v, want nil", bar, scroll)
	}
}

func TestChangeMarksGainedTextIsMarkedLostTextIsNot(t *testing.T) {
	// "abc" gained text from "ab", so it is marked. "a" lost text from "ab", so it
	// is not.
	bar, _ := changeMarks([]string{"ab", "x"}, []string{"abc", "x"})
	if !bar[0] {
		t.Errorf("gained text not marked: %v", bar)
	}
	bar, _ = changeMarks([]string{"ab", "x"}, []string{"a", "x"})
	if bar[0] {
		t.Errorf("lost text marked: %v", bar)
	}
}

func TestIsSubsequence(t *testing.T) {
	if !isSubsequence("ac", "abc") {
		t.Errorf("ac is a subsequence of abc")
	}
	if isSubsequence("ca", "abc") {
		t.Errorf("ca is not a subsequence of abc")
	}
}
