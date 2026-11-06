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
	if d.large != nil {
		// The search lists every match in the document.
		return errLargeAction("find_selection")
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
	e.ensureVisibleForJump(d)
	e.setStatus(search.StatusText(i, len(matches)))
	return nil
}

// maxSearchSelection is the longest selection of a large document that find_selection
// takes as the search text, in bytes. The text is compiled into a pattern on every
// frame its matches are painted, a search reads the document in windows of 1 MiB so
// it cannot find a longer text that lies across two of them, and an error that quotes
// it would be as large as the selection.
const maxSearchSelection = 64 << 10

// errSearchTextTooLarge is the refusal of find_selection on a selection that is too
// large to be the search text of a large document. It matches ErrLargeFile.
func errSearchTextTooLarge() error {
	return &largeError{
		msg:  fmt.Sprintf("selection too large to search for in large file mode (over %d KiB)", maxSearchSelection>>10),
		errs: []error{ErrLargeFile},
	}
}

// searchTextTooLarge reports whether the range from..to, in order, is certainly over
// the size find_selection takes in a large document. It reads no text: the length comes
// from the line index. A range it lets through may still be a little over.
func (d *Document) searchTextTooLarge(from, to buffer.Pos) bool {
	if from.Line == to.Line {
		return to.Col-from.Col > maxSearchSelection
	}
	// The lines between are whole, and the last one is read up to its column, which is
	// at least that many bytes.
	size := to.Col + 1
	for line := from.Line + 1; line < to.Line && size <= maxSearchSelection; line++ {
		size += d.Buffer.LineBytes(line) + 1
	}
	return size > maxSearchSelection
}

// SelectionOrWord returns the text find_selection_next searches for and where it
// lies: the selection, or else the word that the caret is in or just after. ok is
// false when there is neither. A selection that a large document cannot hand out as a
// search text gives ok false and an error that says why, which matches ErrLargeFile.
// The UI turns the text into the active search.
func (e *Editor) SelectionOrWord() (text string, from, to buffer.Pos, ok bool, err error) {
	d := e.Active()
	if d == nil {
		return "", buffer.Pos{}, buffer.Pos{}, false, nil
	}
	from, to, ok = findSelectionRange(d)
	if !ok {
		return "", buffer.Pos{}, buffer.Pos{}, false, nil
	}
	if d.large != nil && d.searchTextTooLarge(from, to) {
		return "", buffer.Pos{}, buffer.Pos{}, false, errSearchTextTooLarge()
	}
	// A large document gives no text that is over the size it hands out.
	text, err = d.readRange(from, to)
	if err == nil && d.large != nil && len(text) > maxSearchSelection {
		err = errSearchTextTooLarge()
	}
	if err != nil {
		return "", buffer.Pos{}, buffer.Pos{}, false, err
	}
	return text, from, to, true, nil
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
