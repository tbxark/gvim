package editor

import (
	"fmt"
	"slices"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/search"
)

// SearchRange returns the range the find actions are limited to, and false when
// they search the whole document. The find bar in the UI reads it too. The range is
// in document coordinates, and any change of text ends it (see guardDocument).
func (e *Editor) SearchRange() (buffer.Selection, bool) {
	d := e.Active()
	if d == nil || d.findRange == nil {
		return buffer.Selection{}, false
	}
	return *d.findRange, true
}

// findInSelection limits the find actions to the selection, in document order. It
// needs a selection, and it changes no text.
func findInSelection(e *Editor, d *Document, _ map[string]any) error {
	if !d.HasSelection() {
		return fmt.Errorf("find_in_selection: %w", ErrNothingSelected)
	}
	from, to := d.Sel.Ordered()
	d.findRange = &buffer.Selection{Anchor: from, Head: to}
	e.setStatus("Search limited to the selection")
	return nil
}

// clearSearch drops the search range. The search text and the highlights belong to the
// find bar in the UI, which is not told by the editor.
func clearSearch(e *Editor, d *Document, _ map[string]any) error {
	d.findRange = nil
	e.setStatus("Search cleared")
	return nil
}

// withinSearchRange keeps the matches that lie inside r. A nil r keeps them all. The
// slice is changed in place, as slices.DeleteFunc does.
func withinSearchRange(matches []search.Match, r *buffer.Selection) []search.Match {
	if r == nil {
		return matches
	}
	return slices.DeleteFunc(matches, func(m search.Match) bool {
		return !inSearchRange(m, *r)
	})
}

// inSearchRange reports whether the match m lies inside r, from its start to its end.
func inSearchRange(m search.Match, r buffer.Selection) bool {
	from, to := r.Ordered()
	return !m.Start.Less(from) && !to.Less(m.End)
}
