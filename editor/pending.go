package editor

import (
	"fmt"
	"maps"
)

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

// gateOnIndex is called before an action runs on d. When d is a large document with
// an incomplete index and the action needs the whole document, it queues the action or
// refuses it, and handled is true.
func (e *Editor) gateOnIndex(d *Document, name Action, args map[string]any) (handled bool, err error) {
	if d.LargeState() == nil || d.Index().Complete {
		return false, nil
	}
	switch {
	case d.WaitsToSelect() && (textChanging[name] || copiesToClipboard[name]):
		// The selection that is queued is not made yet, so an edit or a copy would act
		// on the caret and not on the text the user means.
		return true, fmt.Errorf("%s: %w", name, ErrIndexing)
	case waitsForIndex[name]:
		d.LargeState().Pending = &pending{
			Kind: pendingAction, Name: name, Args: maps.Clone(args),
			Ver: d.Buffer.Version(), Cursor: d.Cursor,
		}
		e.setStatus(scanningStatus(d.Index()))
		return true, nil
	case needsIndexToEdit[name], name == "sort_lines" && !d.HasSelection():
		return true, fmt.Errorf("%s: %w", name, ErrIndexing)
	case joinsNextLine[name] && d.AtFrontierEnd():
		// The line break after the last known line may be followed by a line the index
		// has not found, which the delete would join to this one.
		return true, fmt.Errorf("%s: %w", name, ErrIndexing)
	case frontierSensitive[name] && (!selectionOrLines[name] || !d.HasAnySelection()) && d.OnFrontier():
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
	p := d.LargeState().Pending
	if p == nil {
		return
	}
	info := d.Index()
	switch p.Kind {
	case pendingGoto:
		if p.Line > info.Lines && !info.Complete {
			return
		}
		d.LargeState().Pending = nil
		if e.Active() == d {
			e.extras.jumps.push(p.From)
		}
		e.gotoIn(d, p.Line, p.Col, p.Selecting)
	case pendingAction:
		if !info.Complete {
			return
		}
		d.LargeState().Pending = nil
		if e.Active() != d || d.Buffer.Version() != p.Ver || d.Cursor != p.Cursor {
			d.Status = "Jump cancelled"
			return
		}
		_ = e.Do(p.Name, p.Args)
	}
}
