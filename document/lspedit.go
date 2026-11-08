package document

import (
	"github.com/tbxark/gvim/buffer"
)

// LSPEdit replaces the text from From up to To with Text. Both positions are
// rune positions, 0-based, as buffer.Pos counts them. From equal to To inserts.
type LSPEdit struct {
	From, To buffer.Pos
	Text     string
}
