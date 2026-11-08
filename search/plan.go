package search

import (
	"github.com/tbxark/gvim/buffer"
)

// Edit is one replacement as a text edit: the range From..To of the text before the
// edits, and the text that replaces it.
type Edit struct {
	From, To buffer.Pos
	Text     string
}

// PlanReplace returns the edits ReplaceAll would make for q and repl in b, in document
// order, without changing b. Each match is replaced by the expansion of repl: a literal
// query inserts repl unchanged, and a regex query expands its capture groups from the
// match, after reading the backslash escapes as the characters they stand for.
//
// The panel and the command layer use it to plan a project-wide replace: the edits can
// be applied to the document through the editor's edit path, so a replacement is one
// undo step per file and never writes the offset of a stale match.
func PlanReplace(b *buffer.Buffer, q Query, repl string) ([]Edit, error) {
	r, err := run(b, q)
	if err != nil {
		return nil, err
	}
	if len(r.hits) == 0 {
		return nil, nil
	}
	out := make([]Edit, len(r.hits))
	for i, h := range r.hits {
		out[i] = Edit{From: h.Start, To: h.End, Text: r.expansion(h, q, repl)}
	}
	return out, nil
}
