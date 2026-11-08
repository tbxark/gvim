package textlayout

// GuideScanner computes the indent guide columns of a window of lines, one line at
// a time from the top. It keeps the indentation staircase of the blocks that are
// open: the strictly increasing indent widths of the enclosing lines. A content
// line gets the open blocks strictly to its left, so a file indented by two with a
// tab width of four still has a guide at each of its blocks.
type GuideScanner struct {
	tab   int
	stack []int
}

// NewGuideScanner returns a scanner for tab stops of tab columns whose staircase
// starts as ancestors (see GuideAncestors). A tab below one is treated as one.
func NewGuideScanner(tab int, ancestors []int) *GuideScanner {
	return &GuideScanner{tab: max(tab, 1), stack: append([]int(nil), ancestors...)}
}

// LineIndent returns the display width of the leading blanks of line, and whether
// the line has content. A line of blanks only, or empty, is blank. A carriage
// return counts as nothing.
func LineIndent(line string, tab int) (width int, content bool) {
	tab = max(tab, 1)
	for _, r := range line {
		switch r {
		case ' ':
			width++
		case '\t':
			width += tab - width%tab
		case '\r':
		default:
			return width, true
		}
	}
	return width, false
}

// Content returns the guide columns of a line with content whose indent is width,
// and makes the line the innermost open block.
func (s *GuideScanner) Content(width int) []int {
	for len(s.stack) > 0 && s.stack[len(s.stack)-1] >= width {
		s.stack = s.stack[:len(s.stack)-1]
	}
	cols := s.columns(s.stack, width)
	s.stack = append(s.stack, width)
	return cols
}

// Blank returns the guide columns of a blank line. They are the columns of the
// deeper of the open block and the next content line, whose indent is next (zero
// when there is none), so the guides run through blank lines into a nested body.
// A blank line opens no block.
func (s *GuideScanner) Blank(next int) []int {
	level := next
	if n := len(s.stack); n > 0 {
		level = max(level, s.stack[n-1])
	}
	cut := 0
	for cut < len(s.stack) && s.stack[cut] < level {
		cut++
	}
	return s.columns(s.stack[:cut], level)
}

// Continuation returns the guide columns of a wrapped row whose hanging indent is
// pad columns wide: those of the open blocks that end inside the padding. The
// staircase is not changed.
func (s *GuideScanner) Continuation(pad int) []int {
	cut := 0
	for cut < len(s.stack) && s.stack[cut] < pad {
		cut++
	}
	return s.columns(s.stack[:cut], pad)
}

// columns writes the guide columns for a row of indent width whose open ancestors
// are ancestors. The indent levels left of the shallowest known ancestor have
// scrolled out of view, so they fall back to tab stops.
func (s *GuideScanner) columns(ancestors []int, width int) []int {
	boundary := width
	if len(ancestors) > 0 {
		boundary = ancestors[0]
	}
	var cols []int
	for c := 0; c < boundary; c += s.tab {
		cols = append(cols, c)
	}
	return append(cols, ancestors...)
}

// GuideAncestors returns the staircase of the blocks open above line first: the
// widths of the content lines found going up, each strictly shallower than the one
// before, from the outermost to the innermost. At most maxUp lines are read. line
// gives the text of a line.
func GuideAncestors(line func(i int) string, first, tab, maxUp int) []int {
	var desc []int
	least := -1
	for i, n := first-1, 0; i >= 0 && n < maxUp; i, n = i-1, n+1 {
		w, content := LineIndent(line(i), tab)
		if !content || (least >= 0 && w >= least) {
			continue
		}
		desc = append(desc, w)
		least = w
		if w == 0 {
			break
		}
	}
	for l, r := 0, len(desc)-1; l < r; l, r = l+1, r-1 {
		desc[l], desc[r] = desc[r], desc[l]
	}
	return desc
}
