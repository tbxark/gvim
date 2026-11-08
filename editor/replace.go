package editor

import (
	"errors"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/search"
)

// ReplaceMatch replaces the match m of q with repl in the active document, as one
// undo step, and returns the inserted text. A read-only document is refused with
// ErrReadOnly, and with no tab open the error is ErrNoDocument. The find bar and
// query-replace both replace through here, so the read-only flag is always checked.
func (e *Editor) ReplaceMatch(q search.Query, repl string, m search.Match) (string, error) {
	d, err := e.writableActive()
	if err != nil {
		return "", err
	}
	if d.LargeState() != nil {
		return replaceMatchLarge(e, d, q, repl, m)
	}
	return search.Replace(d.Buffer, q, repl, m)
}

// replaceMatchLarge is ReplaceMatch for a large document. search.ReplaceAt reads only
// the lines of the match, where search.Replace would list every match of the file. A
// refusal (a line that cannot be edited, too many unsaved changes, a match over
// the size that is read to check it) leaves the text as it was and goes to the
// status row in the words of the other large-file refusals.
func replaceMatchLarge(e *Editor, d *Document, q search.Query, repl string, m search.Match) (string, error) {
	out, err := search.ReplaceAt(d.Buffer, q, repl, m)
	switch {
	case err == nil:
		return out, nil
	case errors.Is(err, search.ErrMatchTooLarge):
		err = &largeError{Msg: "replace: match too large for large file mode", Errs: []error{ErrLargeFile, err}}
	case errors.Is(err, buffer.ErrLineTooLong), errors.Is(err, buffer.ErrInvalidUTF8),
		errors.Is(err, buffer.ErrOverlayFull), errors.Is(err, buffer.ErrLineOverBudget), errors.Is(err, buffer.ErrSourceChanged):
		err = d.RefusalError(err)
	default:
		return out, err // no match, or a pattern that does not compile: the caller reports it
	}
	e.setError(err)
	return "", err
}

// ReplaceMatches replaces every match of q with repl in the active document, as
// one undo step, and returns how many it replaced. It refuses a read-only document
// as ReplaceMatch does. With a search range (find_in_selection) only the matches
// inside it are replaced, the same matches the find bar counts.
func (e *Editor) ReplaceMatches(q search.Query, repl string) (int, error) {
	r, limited := e.SearchRange()
	d, err := e.writableActive()
	if err != nil {
		return 0, err
	}
	if d.LargeState() != nil {
		// Replace All lists every match of the document.
		err := &largeError{Msg: "Replace All is not available in large file mode", Errs: []error{ErrLargeFile}}
		e.setError(err)
		return 0, err
	}
	if !limited {
		return search.ReplaceAll(d.Buffer, q, repl)
	}
	// The range is in the text as it is before any replacement, so each match is
	// judged here, once, before the first edit moves the later matches.
	matches, err := search.Find(d.Buffer, q)
	if err != nil {
		return 0, err
	}
	inside := make([]bool, len(matches))
	for i, m := range matches {
		inside[i] = inSearchRange(m, r)
	}
	return search.QueryReplace(d.Buffer, q, repl, func(_ search.Match, i int) search.Decision {
		if inside[i] {
			return search.DecisionReplace
		}
		return search.DecisionSkip
	})
}

// writableActive returns the active document when a replacement may change it. A
// replacement changes the text outside Do, so the record of the last multi-caret
// edit and the search range end here too.
func (e *Editor) writableActive() (*Document, error) {
	d := e.Active()
	var err error
	switch {
	case d == nil:
		err = ErrNoDocument
	case d.ReadOnly():
		err = ErrReadOnly
	default:
		err = d.RefuseEditWhileRestoring("replace")
	}
	if err != nil {
		e.setError(err)
		return nil, err
	}
	d.LastMulti = nil
	d.FindRange = nil
	return d, nil
}
