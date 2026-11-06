package editor

import (
	"errors"
	"fmt"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/search"
)

// registerFindSelHandlers registers find_selection_next and find_selection_previous.
func registerFindSelHandlers(h map[Action]handler) {
	h["find_selection_next"] = func(e *Editor, _ map[string]any) error {
		return findSelection(e, true)
	}
	h["find_selection_previous"] = func(e *Editor, _ map[string]any) error {
		return findSelection(e, false)
	}
}

// findSelection selects the next or previous match of the selected text, or of
// the word at the caret. Matches are literal and case-sensitive, and the search
// wraps around the document. The place before the search is recorded in the
// navigation history.
func findSelection(e *Editor, forward bool) error {
	d := e.Active()
	from, to, ok := findSelectionRange(d)
	if !ok {
		return errors.New("no text to search")
	}
	text := d.Buffer.Range(from, to)
	matches, err := search.Find(d.Buffer, search.Query{Text: text, CaseSensitive: true})
	if err != nil {
		return err
	}
	matches = withinSearchRange(matches, d.findRange)
	var i int
	var found bool
	if forward {
		i, found = search.Next(matches, to, true)
	} else {
		i, found = search.Prev(matches, from, true)
	}
	if !found {
		return fmt.Errorf("no matches for %q", text)
	}
	e.pushJump()
	m := matches[i]
	d.setRange(m.Start, m.End)
	e.setStatus(search.StatusText(i, len(matches)))
	return nil
}

// findSelectionRange returns the range the search starts from: the selection,
// or else the word that the caret is in or just after. ok is false when there
// is neither.
func findSelectionRange(d *Document) (from, to buffer.Pos, ok bool) {
	if d.HasSelection() {
		from, to = d.Sel.Ordered()
		return from, to, true
	}
	p := d.Cursor
	if d.Buffer.ClassAt(p) != buffer.ClassWord {
		if p.Col == 0 {
			return buffer.Pos{}, buffer.Pos{}, false
		}
		p.Col--
		if d.Buffer.ClassAt(p) != buffer.ClassWord {
			return buffer.Pos{}, buffer.Pos{}, false
		}
	}
	from, to = buffer.WordBounds(d.Buffer, p)
	return from, to, true
}
