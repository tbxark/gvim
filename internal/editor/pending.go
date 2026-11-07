package editor

import (
	"fmt"
	"maps"
	"strings"

	"github.com/tbxark/gvim/internal/buffer"
)

// While the line index of a large document is being built, Lines() counts only the
// lines found so far. Work that needs a line the index has not reached, or the end
// of the document, waits in the document as a pending jump and runs when the index
// gets there. Only work that does not change the text waits: an action that edits,
// and needs the end of the document, is refused with ErrIndexing.

type pendingKind int

const (
	pendingGoto pendingKind = iota + 1
	pendingAction
)

// pending is a jump that waits for the line index of a document.
type pending struct {
	kind pendingKind
	// line and col are the 1-based target of a goto.
	line, col int
	// selecting is set when the goto extends the selection in mark mode (GotoLineSelecting).
	selecting bool
	// from is where the caret was when the goto was asked, for the jump list.
	from jumpEntry
	// name and args are the action to run. ver and cursor are the version of the text
	// and the caret when it was asked: the action runs only if both are still the same.
	name   Action
	args   map[string]any
	ver    uint64
	cursor buffer.Pos
}

// waitsForIndex lists the actions that do not change the text and need the whole
// document: they wait while the index is incomplete, and then run as they are.
var waitsForIndex = map[Action]bool{
	"move_document_end":   true,
	"select_document_end": true,
	"select_all":          true,
}

// needsIndexToEdit lists the actions that change the text and need the whole
// document: they return ErrIndexing while the index is incomplete. The actions that a
// large document never does (see refusedInLargeMode) are not here: they are refused
// at once.
var needsIndexToEdit = map[Action]bool{
	"ensure_final_newline": true,
}

// copiesToClipboard lists the actions that put text of the document on the clipboard
// without changing the document.
var copiesToClipboard = map[Action]bool{
	"copy":            true,
	"copy_with_theme": true,
}

// waitsToSelect reports whether a selection (select_all, select_document_end) is queued
// for the index of d.
func (d *Document) waitsToSelect() bool {
	p := d.large.pending
	return p != nil && p.kind == pendingAction && strings.HasPrefix(string(p.name), "select_")
}

// gateOnIndex is called before an action runs on d. When d is a large document with
// an incomplete index and the action needs the whole document, it queues the action or
// refuses it, and handled is true.
func (e *Editor) gateOnIndex(d *Document, name Action, args map[string]any) (handled bool, err error) {
	if d.large == nil || d.Index().Complete {
		return false, nil
	}
	switch {
	case d.waitsToSelect() && (textChanging[name] || copiesToClipboard[name]):
		// The selection that is queued is not made yet, so an edit or a copy would act
		// on the caret and not on the text the user means.
		return true, fmt.Errorf("%s: %w", name, ErrIndexing)
	case waitsForIndex[name]:
		d.large.pending = &pending{
			kind: pendingAction, name: name, args: maps.Clone(args),
			ver: d.Buffer.Version(), cursor: d.Cursor,
		}
		e.setStatus(scanningStatus(d.Index()))
		return true, nil
	case needsIndexToEdit[name], name == "sort_lines" && !d.HasSelection():
		return true, fmt.Errorf("%s: %w", name, ErrIndexing)
	case joinsNextLine[name] && d.atFrontierEnd():
		// The line break after the last known line may be followed by a line the index
		// has not found, which the delete would join to this one.
		return true, fmt.Errorf("%s: %w", name, ErrIndexing)
	case frontierSensitive[name] && (!selectionOrLines[name] || !d.hasAnySelection()) && d.onFrontier():
		// The last line known is not the last line yet, so a line operation on it
		// would treat it as the end of the document.
		return true, fmt.Errorf("%s: %w", name, ErrIndexing)
	}
	return false, nil
}

// resolvePending runs the work that waits for the index of d, when the index has got
// far enough. A goto runs when its line exists, or when the index is complete and the
// line is past the end, and goes to the last line then. An action runs when the index
// is complete, provided the text and the caret are as they were when it was asked, and d
// is the active document. Otherwise it is dropped, with the status "Jump cancelled".
func (e *Editor) resolvePending(d *Document) {
	p := d.large.pending
	if p == nil {
		return
	}
	info := d.Index()
	switch p.kind {
	case pendingGoto:
		if p.line > info.Lines && !info.Complete {
			return
		}
		d.large.pending = nil
		if e.Active() == d {
			e.extras.jumps.push(p.from)
		}
		e.gotoIn(d, p.line, p.col, p.selecting)
	case pendingAction:
		if !info.Complete {
			return
		}
		d.large.pending = nil
		if e.Active() != d || d.Buffer.Version() != p.ver || d.Cursor != p.cursor {
			d.status = "Jump cancelled"
			return
		}
		_ = e.Do(p.name, p.args)
	}
}
