package bundled_test

import (
	"testing"
	"time"
)

// A buffer that ends in a line break has an empty line after it in the host. It is not a
// line of the file, and vi commands must neither land on it nor cross it.
func TestViFinalLineBreak(t *testing.T) {
	t.Parallel()
	const abc = "a\nb\nc\n"
	runViCases(t, []viCase{
		{"G goes to the last line", abc, [2]int{0, 0}, "G", abc, [2]int{2, 0}, ""},
		{"GAx appends to the last line", abc, [2]int{0, 0}, "GAx<Esc>", "a\nb\ncx\n", [2]int{2, 1}, ""},
		{"Gdd deletes the last line", abc, [2]int{0, 0}, "Gdd", "a\nb\n", [2]int{1, 0}, ""},
		{"jjddp", abc, [2]int{0, 0}, "jjddp", abc, [2]int{2, 0}, ""},
		{"Go opens a line after the last", abc, [2]int{0, 0}, "Gox<Esc>", "a\nb\nc\nx\n", [2]int{3, 0}, ""},
		{"Gyyp", abc, [2]int{0, 0}, "Gyyp", "a\nb\nc\nc\n", [2]int{3, 0}, ""},
		{"ggyyGp", abc, [2]int{0, 0}, "ggyyGp", "a\nb\nc\na\n", [2]int{3, 0}, ""},
		{"} in the last paragraph stops on the last line", abc, [2]int{0, 0}, "}", abc, [2]int{2, 0}, ""},
		{"L", abc, [2]int{0, 0}, "L", abc, [2]int{2, 0}, ""},
		{":$", abc, [2]int{0, 0}, ":«$»", abc, [2]int{2, 0}, ""},
		{"j stops on the last line", abc, [2]int{0, 0}, "jjj", abc, [2]int{2, 0}, ""},
		{"<Down> stops on the last line", abc, [2]int{0, 0}, "<Down><Down><Down>", abc, [2]int{2, 0}, ""},
		{"dG keeps the final line break", abc, [2]int{1, 0}, "dG", "a\n", [2]int{0, 0}, ""},
		{"10dd keeps the final line break", abc, [2]int{1, 0}, "10dd", "a\n", [2]int{0, 0}, ""},
		{"d5j keeps the final line break", abc, [2]int{1, 0}, "d5j", "a\n", [2]int{0, 0}, ""},
		{":.,$d", abc, [2]int{1, 0}, ":«.,$d»", "a\n", [2]int{0, 0}, ""},
		{":1,$-1d", abc, [2]int{0, 0}, ":«1,$-1d»", "c\n", [2]int{0, 0}, ""},
		{"J on the last line does nothing", abc, [2]int{2, 0}, "J", abc, [2]int{2, 0}, ""},
		{"5J joins up to the last line", abc, [2]int{0, 0}, "5J", "a b c\n", [2]int{0, 3}, ""},
		{"ggVGJ", abc, [2]int{0, 0}, "VGJ", "a b c\n", [2]int{0, 3}, ""},
		{"vGd", abc, [2]int{1, 0}, "vGd", "a\n\n", [2]int{1, 0}, ""},
		{"cG", abc, [2]int{1, 0}, "cGx<Esc>", "a\nx\n", [2]int{1, 0}, ""},
		{"ggdG empties the buffer", abc, [2]int{0, 0}, "dG", "", [2]int{0, 0}, ""},
		{"a buffer without a final line break is unchanged", "a\nb\nc", [2]int{0, 0}, "GAx<Esc>", "a\nb\ncx", [2]int{2, 1}, ""},
		{"o on the last line of a file without a final line break", "a", [2]int{0, 0}, "ox<Esc>", "a\nx", [2]int{1, 0}, ""},
		{"a blank last line is a line", "a\n\n\n", [2]int{0, 0}, "G", "a\n\n\n", [2]int{2, 0}, ""},
		{"dd on the only line", "a\n", [2]int{0, 0}, "dd", "", [2]int{0, 0}, ""},
		{"%s/^/X/ does not add a line", "a\nb\n", [2]int{0, 0}, ":«%s/^/> /»", "> a\n> b\n", [2]int{1, 0}, ""},
		{"%s/$/;/ does not add a line", "a\nb\n", [2]int{0, 0}, ":«%s/$/;/»", "a;\nb;\n", [2]int{1, 0}, ""},
		{":sort", "c\nb\na\n", [2]int{0, 0}, ":«sort»", "a\nb\nc\n", [2]int{0, 0}, ""},
		{":sort u", "c\nb\nb\na\n", [2]int{0, 0}, ":«sort u»", "a\nb\nc\n", [2]int{0, 0}, ""},
		{":sort!", "a\nb\nc\n", [2]int{0, 0}, ":«sort!»", "c\nb\na\n", [2]int{0, 0}, ""},
	})
}

// A text object asked for again in the visual mode grows the area.
func TestViVisualObjectsExtend(t *testing.T) {
	t.Parallel()
	const words = "one two three four\n"
	runViCases(t, []viCase{
		{"vawaw", words, [2]int{0, 4}, "vawawd", "one four\n", [2]int{0, 4}, ""},
		{"vawawaw", words, [2]int{0, 4}, "vawawawd", "one \n", [2]int{0, 3}, ""},
		{"viwiw", words, [2]int{0, 4}, "viwiwd", "one three four\n", [2]int{0, 4}, ""},
		{"viwiwiw", words, [2]int{0, 4}, "viwiwiwd", "one  four\n", [2]int{0, 4}, ""},
		{"vi(i(", "f(a, (b, c), x) y\n", [2]int{0, 7}, "vi(i(d", "f() y\n", [2]int{0, 2}, ""},
		{"vi(a(", "f(a, (b, c), x) y\n", [2]int{0, 7}, "vi(a(d", "f(a, , x) y\n", [2]int{0, 5}, ""},
		{"vi\"i\" takes the quotes too", "say \"hi there\" now\n", [2]int{0, 6}, "vi\"i\"d", "say  now\n", [2]int{0, 4}, ""},
		{"vitit", "<a><b>xy</b></a>\n", [2]int{0, 6}, "vititd", "<a></a>\n", [2]int{0, 3}, ""},
		{"vipip", "a\nb\n\nc\nd\n\ne\n", [2]int{0, 0}, "vipipd", "c\nd\n\ne\n", [2]int{0, 0}, ""},
		{"vapap", "a\nb\n\nc\nd\n\ne\n", [2]int{0, 0}, "vapapd", "e\n", [2]int{0, 0}, ""},
		{"a single object still works", words, [2]int{0, 4}, "vawd", "one three four\n", [2]int{0, 4}, ""},
		{"v2aw", words, [2]int{0, 4}, "v2awd", "one four\n", [2]int{0, 4}, ""},
	})
}

// $ makes j and k keep to the end of the line, until something else moves the caret.
func TestViDollarSticksToTheEndOfLines(t *testing.T) {
	t.Parallel()
	const text = "short\nlonger line here\nmid line\nx\n"
	runViCases(t, []viCase{
		{"$j", text, [2]int{0, 0}, "$j", text, [2]int{1, 15}, ""},
		{"$jj", text, [2]int{0, 0}, "$jj", text, [2]int{2, 7}, ""},
		{"$jjk", text, [2]int{0, 0}, "$jjk", text, [2]int{1, 15}, ""},
		{"$ then an insert", text, [2]int{0, 0}, "$jiX<Esc>", "short\nlonger line herXe\nmid line\nx\n", [2]int{1, 15}, ""},
		{"a plain j keeps the column", text, [2]int{0, 3}, "jj", text, [2]int{2, 3}, ""},
		{"another move ends it", text, [2]int{0, 0}, "$hj", text, [2]int{1, 3}, ""},
		{"a command in between ends it", text, [2]int{0, 0}, "$xj", "shor\nlonger line here\nmid line\nx\n", [2]int{1, 3}, ""},
	})
}

// A combining mark belongs to the character before it.
func TestViCombiningMarks(t *testing.T) {
	t.Parallel()
	const text = "e\u0301x ab\n"
	runViCases(t, []viCase{
		{"x deletes the base and its mark", text, [2]int{0, 0}, "x", "x ab\n", [2]int{0, 0}, ""},
		{"r replaces the base and its mark", text, [2]int{0, 0}, "r-", "-x ab\n", [2]int{0, 0}, ""},
		{"l skips the mark", text, [2]int{0, 0}, "lx", "e\u0301 ab\n", [2]int{0, 3}, ""},
		{"h goes back over the mark", text, [2]int{0, 3}, "h", text, [2]int{0, 0}, ""},
		{"~ toggles the base", "e\u0301x\n", [2]int{0, 0}, "~", "E\u0301x\n", [2]int{0, 3}, ""},
		{"$ lands on the last character", "ae\u0301\n", [2]int{0, 0}, "$x", "a\n", [2]int{0, 0}, ""},
	})
}

// ctrl+d and ctrl+u scroll half a window and move the caret as many lines, so it stays on
// the same row of the window.
func TestViHalfPageScroll(t *testing.T) {
	t.Parallel()
	run := func(t *testing.T, top, line int, keys string) (int, int) {
		v := newVi(t, numberedLines(200))
		v.scrolls = true
		w := v.host.Window(1)
		w.TopLine = top
		v.at(line, 0)
		v.keys(keys)
		return v.cursor().Line, w.TopLine
	}
	half := func() int { return newVi(t, "x").host.Window(1).Height / 2 }()
	tests := []struct {
		name      string
		top, line int
		keys      string
		wantLine  int // relative to line
		wantTop   int // relative to top
	}{
		{"ctrl+d from the top", 0, 0, "<C-d>", half, half},
		{"ctrl+d twice", 0, 0, "<C-d><C-d>", 2 * half, 2 * half},
		{"ctrl+d keeps the row", 20, 25, "<C-d>", half, half},
		{"ctrl+u keeps the row", 40, 45, "<C-u>", -half, -half},
		{"ctrl+d then ctrl+u is a round trip", 20, 25, "<C-d><C-u>", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			line, top := run(t, tt.top, tt.line, tt.keys)
			if line != tt.line+tt.wantLine || top != tt.top+tt.wantTop {
				t.Errorf("line, top = %d, %d; want %d, %d", line, top, tt.line+tt.wantLine, tt.top+tt.wantTop)
			}
		})
	}
}

// A command over the whole of a big file has to fit in the budget of a slice, or the plugin is
// struck for it. Joining by concatenation copied the result once for each line, which took
// seconds for 50 000 lines; the budget here is looser than the slice of the editor (500 ms)
// so that a busy machine does not fail the test.
func TestViBigRangesFitTheBudget(t *testing.T) {
	t.Parallel()
	const n = 50000
	text := numberedLines(n) + "\n"
	for _, keys := range []string{"ggVGJ", ":«%j»", "gg50000J", "g~G", "ggg?G"} {
		t.Run(keys, func(t *testing.T) {
			budget := 1500 * time.Millisecond
			if raceDetector {
				budget *= 10
			}
			v := newViTimeout(t, text, budget)
			v.keys(keys)
			if p, _ := v.h.Plugin(viPlugin); p.Status != "loaded" {
				t.Fatalf("plugin is %q after %s: %v", p.Status, keys, v.h.LogText(viPlugin))
			}
			if got := v.text(); got == text {
				t.Errorf("%s changed nothing: it did not finish", keys)
			}
		})
	}
}

// . repeats a block insert, append and change on a block of the same size at the caret.
func TestViDotRepeatsBlockEdits(t *testing.T) {
	t.Parallel()
	const four = "col1 col2\ncol1 col2\ncol1 col2\ncol1 col2\n"
	runViCases(t, []viCase{
		{"block I", four, [2]int{0, 0}, "<C-v>jIX<Esc>jj.", "Xcol1 col2\nXcol1 col2\nXcol1 col2\nXcol1 col2\n", [2]int{2, 0}, ""},
		{"block A", four, [2]int{0, 3}, "<C-v>jAX<Esc>jj.", "col1X col2\ncol1X col2\ncol1X col2\ncol1X col2\n", [2]int{2, 3}, ""},
		{"block A to the end of the lines", four, [2]int{0, 0}, "<C-v>j$A;<Esc>jj.", "col1 col2;\ncol1 col2;\ncol1 col2;\ncol1 col2;\n", [2]int{2, 0}, ""},
		{"block c", four, [2]int{0, 0}, "<C-v>jlcZ<Esc>jj.", "Zl1 col2\nZl1 col2\nZl1 col2\nZl1 col2\n", [2]int{2, 0}, ""},
	})
}
