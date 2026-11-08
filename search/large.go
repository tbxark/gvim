package search

import (
	"errors"
	"strings"

	"github.com/tbxark/gvim/buffer"
)

// ErrLargeBuffer is returned by Find, Replace, ReplaceAll and QueryReplace when
// the buffer is in large file mode. They read every line of the buffer and
// build a list of all matches, which a large file cannot afford. The bounded
// replacements are FindNext (go to the next or previous match) and FindVisible
// (highlights for the rows on screen).
var ErrLargeBuffer = errors.New("search: not available on large buffers")

// newWindow builds the document for text, which holds the buffer lines from
// first on, joined by "\n". Positions it returns carry buffer line numbers.
func newWindow(first int, text string) document {
	starts := []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return document{text: text, lineStart: starts, first: first}
}

// FindVisible returns the matches of q that touch rows first through last
// (inclusive), in document order, with buffer line numbers. rows returns line
// i without its newline. It reads only the visible rows plus as many again
// above and below them, so a multi-line match that starts just off screen is
// still seen, and it never searches the rest of the document. A match that
// starts further than that above the rows is not seen.
//
// rows may be asked for lines past the end of the document and should return
// "" for them; a match that runs onto such lines is for the caller to clip, as
// Spans does. A negative first is taken as 0, and first after last returns nil.
func FindVisible(rows func(i int) string, first, last int, q Query) ([]Match, error) {
	first = max(first, 0)
	if first > last {
		return nil, nil
	}
	height := last - first + 1
	lo := max(first-height, 0)
	hi := last + height
	var sb strings.Builder
	for i := lo; i <= hi; i++ {
		if i > lo {
			sb.WriteByte('\n')
		}
		sb.WriteString(rows(i))
	}
	// The temporary buffer is a memory buffer, so Find is not refused.
	found, err := Find(buffer.New(sb.String()), q)
	if err != nil {
		return nil, err
	}
	var out []Match
	for _, m := range found {
		m.Start.Line += lo
		m.End.Line += lo
		if m.End.Line >= first && m.Start.Line <= last {
			out = append(out, m)
		}
	}
	return out, nil
}
