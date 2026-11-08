package editorpane

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// hintDoc is a matchDoc that says which lines were edited, as the editor's document does, and
// the rune length of its lines.
type hintDoc struct {
	*matchDoc
	lo, hi int
	ok     bool
}

func (d *hintDoc) EditedLines() (int, int, bool) { return d.lo, d.hi, d.ok }
func (d *hintDoc) LineLen(i int) int             { return len([]rune(d.lines[i])) }

// seq returns the integers from a up to, not including, b.
func seq(a, b int) []int {
	var out []int
	for i := a; i < b; i++ {
		out = append(out, i)
	}
	return out
}

// numberedLines returns n distinct lines.
func numberedLines(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = strings.Repeat("l", i%5) + string(rune('a'+i%26))
	}
	return out
}

// The change mark of the last column is worked out for the rows on screen: a line that differs from
// the saved line at its index, below the window, has none worked out.
func TestScrollMarksAreForTheRowsOnScreen(t *testing.T) {
	t.Parallel()
	saved := numberedLines(100)
	cur := append([]string(nil), saved...)
	cur[52], cur[90] = "changed", "also changed"
	d := &matchDoc{fakeDoc: &fakeDoc{lines: cur, saved: saved, modified: true}}
	f := frame{doc: d, lines: len(cur), top: 50, h: 5}
	_, scroll := f.changes()
	if !maps.Equal(scroll, map[int]bool{52: true}) {
		t.Errorf("scroll marks = %v, want only the line on screen", scroll)
	}
}

// With a line more or fewer, the lines after the edit differ from the saved line at their index,
// and the row past the end is marked when it shows.
func TestScrollMarksWhenTheLineCountChanged(t *testing.T) {
	t.Parallel()
	saved := numberedLines(6)
	cur := append(append([]string(nil), saved[:2]...), append([]string{"new"}, saved[2:]...)...)
	d := &matchDoc{fakeDoc: &fakeDoc{lines: cur, saved: saved, modified: true}}
	_, want := changeMarks(saved, cur)
	f := frame{doc: d, lines: len(cur), top: 0, h: len(cur) + 1}
	if _, scroll := f.changes(); !maps.Equal(scroll, want) {
		t.Errorf("scroll marks = %v, want %v", scroll, want)
	}
	// The row past the end is not on screen, so it has no mark worked out.
	f = frame{doc: d, lines: len(cur), top: 0, h: 3}
	if _, scroll := f.changes(); !maps.Equal(scroll, map[int]bool{2: true}) {
		t.Errorf("scroll marks of the first rows = %v, want line 2", scroll)
	}
}

// A document that says which lines were edited has only those compared and built, also when it
// has more or fewer lines than the saved text, and the marks are what comparing all lines gives.
func TestEditedLinesNarrowTheComparison(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		edit      func(cur []string) []string
		lo, hi    int
		wantBuilt []int
	}{
		{"one line", func(c []string) []string { c[40] = "edited"; return c }, 40, 41, []int{40}},
		{"a line added", func(c []string) []string {
			return append(c[:40], append([]string{"new"}, c[40:]...)...)
		}, 40, 41, []int{40}},
		{"a line removed", func(c []string) []string { return append(c[:40], c[41:]...) }, 40, 40, nil},
		{"edits at both ends", func(c []string) []string { c[2], c[90] = "x", "y"; return c }, 2, 91, seq(2, 91)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			saved := numberedLines(100)
			cur := tt.edit(append([]string(nil), saved...))
			d := &hintDoc{matchDoc: &matchDoc{fakeDoc: &fakeDoc{lines: cur, saved: saved, modified: true}}, lo: tt.lo, hi: tt.hi, ok: true}
			f := frame{doc: d, lines: len(cur), top: 0, h: len(cur) + 1}
			bar, _ := f.changes()
			wantBar, _ := changeMarks(saved, cur)
			if !maps.Equal(bar, wantBar) {
				t.Errorf("bar marks = %v, want %v", bar, wantBar)
			}
			if !slices.Equal(d.built, tt.wantBuilt) && (len(d.built) != 0 || len(tt.wantBuilt) != 0) {
				t.Errorf("built lines %v, want %v", d.built, tt.wantBuilt)
			}
		})
	}
}

// An edited line that is too long to compare is not read: it is marked, unless it got shorter.
func TestVeryLongEditedLineIsMarkedWithoutReadingIt(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", longMarkRunes+10)
	tests := []struct {
		name      string
		saved     string
		cur       string
		wantMarks bool
	}{
		{"grown", long, long + "y", true},
		{"changed", long, strings.Repeat("z", len(long)), true},
		{"shorter", long, long[:len(long)-5], false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			saved := []string{"a", tt.saved, "c"}
			cur := []string{"a", tt.cur, "c"}
			d := &hintDoc{matchDoc: &matchDoc{fakeDoc: &fakeDoc{lines: cur, saved: saved, modified: true}}, lo: 1, hi: 2, ok: true}
			f := frame{doc: d, lines: 3, top: 0, h: 4}
			bar, scroll := f.changes()
			if got := bar[1]; got != tt.wantMarks {
				t.Errorf("bar mark = %v, want %v", got, tt.wantMarks)
			}
			if !scroll[1] {
				t.Errorf("scroll marks = %v, want line 1", scroll)
			}
			for _, i := range d.built {
				if i == 1 {
					t.Error("the long line was built as a string")
				}
			}
		})
	}
}

// A long line in an edited window is not read when the edit is elsewhere in the window: it is
// marked, as an edited long line is, and the lines of the window that are not long are compared.
// Typing at the end of a document with a long line above the caret costs the same on every key.
func TestLongLineInAnEditedWindowIsNotRead(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", longMarkRunes+10)
	saved := []string{long, "b", "c"}
	cur := []string{long + "y", "b", "CHANGED"}
	d := &hintDoc{matchDoc: &matchDoc{fakeDoc: &fakeDoc{lines: cur, saved: saved, modified: true}}, lo: 0, hi: 3, ok: true}
	f := frame{doc: d, lines: 3, top: 0, h: 4}
	bar, _ := f.changes()
	if !slices.Contains(d.built, 2) {
		t.Errorf("built lines %v, want line 2 built", d.built)
	}
	if slices.Contains(d.built, 0) {
		t.Error("the long line was built as a string")
	}
	if !bar[0] || !bar[2] || bar[1] {
		t.Errorf("bar marks = %v, want lines 0 and 2 only", bar)
	}
}

// An unchanged long line in an edited window is not marked: it is compared with its saved text
// without being built.
func TestUnchangedLongLineInAnEditedWindowIsNotMarked(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", longMarkRunes+10)
	saved := []string{"a", long, "c"}
	cur := []string{"a", long, "CHANGED"}
	d := &hintDoc{matchDoc: &matchDoc{fakeDoc: &fakeDoc{lines: cur, saved: saved, modified: true}}, lo: 0, hi: 3, ok: true}
	f := frame{doc: d, lines: 3, top: 0, h: 4}
	bar, _ := f.changes()
	if slices.Contains(d.built, 1) {
		t.Error("the unchanged long line was built as a string")
	}
	if bar[1] || !bar[2] {
		t.Errorf("bar marks = %v, want line 2 only", bar)
	}
}

// BenchmarkChangeMarksWithALongLineInTheWindow is the cost of one frame's change marks when a line
// of 5 MB is in the edited window, and a short line below it is edited too. The long line is
// not read, so the cost does not follow its length.
func BenchmarkChangeMarksWithALongLineInTheWindow(b *testing.B) {
	long := strings.Repeat("x", 5<<20)
	saved := []string{long, "b", "c"}
	cur := []string{long + "y", "b", "CHANGED"}
	d := lenDoc{&hintDoc{matchDoc: &matchDoc{fakeDoc: &fakeDoc{lines: cur, saved: saved, modified: true}}, lo: 0, hi: 3, ok: true}}
	f := frame{doc: d, lines: 3, top: 0, h: 4}
	b.ResetTimer()
	for range b.N {
		f.changes()
	}
}

// lenDoc is a hintDoc whose line length is read in no time, as the editor's document reads it, for
// texts of one-byte runes.
type lenDoc struct{ *hintDoc }

func (d lenDoc) LineLen(i int) int { return len(d.lines[i]) }
