package editorpane

// matchKind is the search highlight of a cell. The order is the paint priority:
// a cell in both an ordinary match and the current match takes the current one,
// and an occurrence of the word at the caret loses to any search match.
type matchKind uint8

const (
	matchNone       matchKind = iota // not in a search match or an occurrence
	matchOccurrence                  // an occurrence of the word at the caret
	matchOther                       // in a search match that is not the current one
	matchCurrent                     // in the current search match
)

// markMatches sets the search match of each cell of line i that a highlight
// covers. Highlight columns are rune columns of raw, so each one maps to the
// expanded cell it starts at through expandedIndex, as the caret does. A tab
// that a highlight covers marks all of its blanks. A column past the end of the
// line maps to the end of its cells, so a highlight that runs past the line is
// clipped to it.
func (f frame) markMatches(i int, raw []rune, cells []cell) {
	if f.highlights == nil {
		return
	}
	for _, h := range f.highlights(i) {
		kind := matchOther
		if h.Current {
			kind = matchCurrent
		}
		from, to := expandedIndex(raw, h.Start, f.tab), expandedIndex(raw, h.End, f.tab)
		for k := from; k < to; k++ {
			cells[k].match = max(cells[k].match, kind)
		}
	}
}
