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

// matchDoc is a fakeDoc that compares lines in place, as the editor's adapter does, and
// records the lines the pane builds as strings.
type matchDoc struct {
	*fakeDoc
	built []int
}

func (d *matchDoc) Line(i int) string {
	d.built = append(d.built, i)
	return d.fakeDoc.Line(i)
}
func (d *matchDoc) LineIs(i int, text string) bool { return d.lines[i] == text }

// The change marks of a document that compares lines in place equal changeMarks of all
// the lines, as far as they are drawn, and only the lines between the first and the last
// change are built, also when the line count changed.
func TestFrameChangesBuildsOnlyTheChangedLines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		saved, cur []string
		wantBuilt  []int
	}{
		{"edit in the middle", []string{"a", "b", "c", "d", "e"}, []string{"a", "b", "C", "d", "e"}, []int{2}},
		{"two edits", []string{"a", "b", "c", "d", "e"}, []string{"A", "b", "c", "d", "E"}, []int{0, 1, 2, 3, 4}},
		{"gained text at the end", []string{"a", "b", "c"}, []string{"a", "b", "cc"}, []int{2}},
		{"nothing differs", []string{"a", "b"}, []string{"a", "b"}, []int{}},
		{"line added", []string{"a", "b", "c"}, []string{"a", "b", "x", "c"}, []int{2}},
		{"line removed", []string{"a", "b", "c"}, []string{"a", "c"}, []int{}},
		{"non-ASCII line", []string{"é", "日本", "x"}, []string{"é", "日本語", "x"}, []int{1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := &matchDoc{fakeDoc: &fakeDoc{lines: tc.cur, saved: tc.saved, modified: true}}
			// The rows on screen cover every line and the row past the last one.
			f := frame{doc: d, lines: len(tc.cur), h: len(tc.cur) + 1}
			bar, scroll := f.changes()
			wantBar, wantScroll := changeMarks(tc.saved, tc.cur)
			if !maps.Equal(bar, wantBar) || !maps.Equal(scroll, wantScroll) {
				t.Errorf("marks = %v, %v; want %v, %v", bar, scroll, wantBar, wantScroll)
			}
			if len(d.built) != len(tc.wantBuilt) {
				t.Fatalf("built lines %v, want %v", d.built, tc.wantBuilt)
			}
			for i := range d.built {
				if d.built[i] != tc.wantBuilt[i] {
					t.Fatalf("built lines %v, want %v", d.built, tc.wantBuilt)
				}
			}
		})
	}
}
