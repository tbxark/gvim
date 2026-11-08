package search

import (
	"fmt"
	"sort"

	"github.com/tbxark/gvim/buffer"
)

// Next returns the index in matches of the first match that starts at or after
// from. matches must come from Find, in document order. Pass the end of the
// current selection as from, so a match that starts exactly there counts as
// next. With wrap set, a search past the last match returns index 0. It
// reports false when there is no match to go to.
func Next(matches []Match, from buffer.Pos, wrap bool) (int, bool) {
	i := sort.Search(len(matches), func(i int) bool {
		return !matches[i].Start.Less(from)
	})
	if i < len(matches) {
		return i, true
	}
	if wrap && len(matches) > 0 {
		return 0, true
	}
	return -1, false
}

// Prev returns the index in matches of the last match that starts before from.
// matches must come from Find, in document order. Pass the start of the current
// selection as from, so the selected match is not returned again. With wrap
// set, a search before the first match returns the last index. It reports
// false when there is no match to go to.
func Prev(matches []Match, from buffer.Pos, wrap bool) (int, bool) {
	i := sort.Search(len(matches), func(i int) bool {
		return !matches[i].Start.Less(from)
	}) - 1
	if i >= 0 {
		return i, true
	}
	if wrap && len(matches) > 0 {
		return len(matches) - 1, true
	}
	return -1, false
}

// StatusText formats the find status for the match at index, which Next or
// Prev returned, out of total matches. It gives "Match 2 of 5", or "No matches"
// when total is zero. An index outside the matches gives "N matches".
func StatusText(index, total int) string {
	switch {
	case total == 0:
		return "No matches"
	case index < 0 || index >= total:
		return fmt.Sprintf("%d matches", total)
	}
	return fmt.Sprintf("Match %d of %d", index+1, total)
}
