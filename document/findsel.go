package document

import (
	"fmt"

	"github.com/tbxark/gvim/buffer"
)

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
		Msg:  fmt.Sprintf("selection too large to search for in large file mode (over %d KiB)", maxSearchSelection>>10),
		Errs: []error{ErrLargeFile},
	}
}

// SearchTextTooLarge reports whether the range from..to, in order, is certainly over
// the size find_selection takes in a large document. It reads no text: the length comes
// from the line index. A range it lets through may still be a little over.
func (d *Document) SearchTextTooLarge(from, to buffer.Pos) bool {
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
