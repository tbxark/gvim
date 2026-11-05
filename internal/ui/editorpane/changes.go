package editorpane

import "slices"

// changeMarks compares the saved lines with the current lines and returns the
// indexes that carry the two change marks:
//
//   - bar: the margin bar "│" in the gutter's first column. A line is marked when
//     it is new, or when it gained text relative to the saved line it replaces.
//     A line that only lost text is not marked.
//   - scroll: the change mark in the last column. A line is marked when its text
//     differs from the saved line at the same index. When the line count changed,
//     the row just past the last line is marked too.
//
// Both maps are nil when nothing differs.
func changeMarks(saved, cur []string) (bar, scroll map[int]bool) {
	for i := range cur {
		if i >= len(saved) || cur[i] != saved[i] {
			if scroll == nil {
				scroll = make(map[int]bool)
			}
			scroll[i] = true
		}
	}
	if scroll != nil && len(cur) != len(saved) {
		scroll[len(cur)] = true
	}
	bar = barMarks(saved, cur)
	return bar, scroll
}

// barMarks aligns the saved and current lines and marks the current lines that
// are new or gained text. Common prefix and suffix lines are skipped first. In
// the middle, the longest common subsequence of equal lines anchors the
// alignment. Between anchors, the current lines are paired with saved lines from
// the end of each gap. A paired line is marked when it is not a pure deletion of
// its saved partner, and an unpaired line is always marked.
func barMarks(saved, cur []string) map[int]bool {
	var bar map[int]bool
	mark := func(i int) {
		if bar == nil {
			bar = make(map[int]bool)
		}
		bar[i] = true
	}

	p := 0
	for p < len(saved) && p < len(cur) && saved[p] == cur[p] {
		p++
	}
	s := 0
	for s < len(saved)-p && s < len(cur)-p && saved[len(saved)-1-s] == cur[len(cur)-1-s] {
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
			case v == b[j]:
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
		case a[i] == b[j]:
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
			if ai < ai0 || !isSubsequence(b[bi], a[ai]) {
				mark(p + bi)
			}
		}
		ai0, bj0 = an.ai+1, an.bj+1
	}
	return bar
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
