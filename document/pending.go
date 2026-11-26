package document

import (
	"github.com/tbxark/gvim/buffer"
	"strings"
)

type pendingKind int

const (
	PendingGoto pendingKind = iota + 1
	PendingAction
)

// pending is a jump that waits for the line index of a document.
type Pending struct {
	Kind pendingKind
	// line and col are the 1-based target of a goto.
	Line, Col int
	// selecting is set when the goto extends the selection in mark mode (GotoLineSelecting).
	Selecting bool
	// from is where the caret was when the goto was asked, for the jump list.
	From JumpEntry
	// name and args are the action to run. ver and cursor are the version of the text
	// and the caret when it was asked: the action runs only if both are still the same.
	Name   Action
	Args   map[string]any
	Ver    uint64
	Cursor buffer.Pos
}

// WaitsToSelect reports whether a selection (select_all, select_document_end) is queued
// for the index of d.
func (d *Document) WaitsToSelect() bool {
	if d.large == nil {
		return false
	}
	p := d.large.Pending
	return p != nil && p.Kind == PendingAction && strings.HasPrefix(string(p.Name), "select_")
}
