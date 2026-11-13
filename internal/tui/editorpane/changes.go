package editorpane

import "slices"

// curLine is a current line for barMarks. A line the caller did not read, because it is too
// long to compare, has its length in runes instead of its text: it equals no saved line, and
// as a paired line it is marked unless it got shorter than its saved partner.
type curLine struct {
	text  string
	runes int
	long  bool
}

// barMarks aligns the saved and current lines and marks the current lines that
// are new or gained text. Common prefix and suffix lines are skipped first. In
// the middle, the longest common subsequence of equal lines anchors the
// alignment. Between anchors, the current lines are paired with saved lines from
// the end of each gap. A paired line is marked when it is not a pure deletion of
// its saved partner, and an unpaired line is always marked.
func barMarks(saved []string, cur []curLine) map[int]bool {
	var bar map[int]bool
	mark := func(i int) {
		if bar == nil {
			bar = make(map[int]bool)
		}
		bar[i] = true
	}

	p := 0
	for p < len(saved) && p < len(cur) && cur[p].is(saved[p]) {
		p++
	}
	s := 0
	for s < len(saved)-p && s < len(cur)-p && cur[len(cur)-1-s].is(saved[len(saved)-1-s]) {
		s++
	}
	a := saved[p : len(saved)-s]
	b := cur[p : len(cur)-s]
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	const maxCells = 1 << 22 // beyond this, the middle is treated as all new
	if len(a)*len(b) > maxCells {
		for i := range b {
			mark(p + i)
		}
		return bar
	}

	// table holds the LCS lengths of the middle; the walk below backtracks anchors.
	table := make([][]int32, len(a)+1)
	for i := range table {
		table[i] = make([]int32, len(b)+1)
	}
	for i, v := range slices.Backward(a) {
		for j := len(b) - 1; j >= 0; j-- {
			switch {
			case b[j].is(v):
				table[i][j] = table[i+1][j+1] + 1
			case table[i+1][j] >= table[i][j+1]:
				table[i][j] = table[i+1][j]
			default:
				table[i][j] = table[i][j+1]
			}
		}
	}
	type anchor struct{ ai, bj int }
	var anchors []anchor
	for i, j := 0, 0; i < len(a) && j < len(b); {
		switch {
		case b[j].is(a[i]):
			anchors = append(anchors, anchor{i, j})
			i++
			j++
		case table[i+1][j] >= table[i][j+1]:
			i++
		default:
			j++
		}
	}

	// Walk the gaps between anchors. The sentinel closes the last gap.
	anchors = append(anchors, anchor{len(a), len(b)})
	ai0, bj0 := 0, 0
	for _, an := range anchors {
		// The gap holds a[ai0:an.ai] and b[bj0:an.bj]. Pair from the end.
		for bi := an.bj - 1; bi >= bj0; bi-- {
			t := an.bj - 1 - bi
			ai := an.ai - 1 - t
			if ai < ai0 || !b[bi].keeps(a[ai]) {
				mark(p + bi)
			}
		}
		ai0, bj0 = an.ai+1, an.bj+1
	}
	return bar
}

// is reports whether the line was read and is the text s.
func (c curLine) is(s string) bool { return !c.long && c.text == s }

// keeps reports whether the line only lost text relative to its saved partner a. A line that
// was read keeps when it is a subsequence of a; a line that was not read keeps when it is shorter.
func (c curLine) keeps(a string) bool {
	if c.long {
		return c.runes < len(a)
	}
	return isSubsequence(c.text, a)
}

// isSubsequence reports whether every rune of short appears in long in order. A
// line that is a subsequence of its saved partner only lost text.
func isSubsequence(short, long string) bool {
	rs := []rune(short)
	i := 0
	for _, r := range long {
		if i < len(rs) && rs[i] == r {
			i++
		}
	}
	return i == len(rs)
}
