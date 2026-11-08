package editor

import (
	"fmt"
)

// toggleReadOnly turns the read-only flag of the document on or off.
func toggleReadOnly(e *Editor, d *Document, _ map[string]any) error {
	if d.Binary() {
		return fmt.Errorf("toggle_read_only: %w", ErrBinary)
	}
	d.SetReadOnly(!d.ReadOnly())
	if d.ReadOnly() {
		e.setStatus("Read-only mode on")
	} else {
		e.setStatus("Read-only mode off")
	}
	return nil
}

// registerDocumentHandlers adds the document-state actions to h, then wraps every
// action in h with guardDocument. buildHandlers calls it last, so the wrap covers
// the handlers registered before it.
func registerDocumentHandlers(h map[Action]handler) {
	h["toggle_read_only"] = docAction(toggleReadOnly)
	h["toggle_fold"] = docAction(toggleFold)
	h["set_encoding"] = docAction(setEncoding)
	h["set_line_ending"] = docAction(setLineEnding)
	h["reload_with_encoding"] = docAction(reloadWithEncoding)
	for name, fn := range h {
		h[name] = guardDocument(name, fn)
	}
}

// guardDocument wraps the handler of one action. A read-only document refuses a
// text-changing action before the action runs. Any other text-changing action
// ends the record of the last multi-caret edit (see multiEditRecord), because the
// record would no longer match the undo history. Undo and redo keep it. After
// the action, the carets are moved off collapsed folds (see revealCarets) on the
// tab that is active then. That tab may differ from the one active before.
func guardDocument(name Action, fn handler) handler {
	return func(e *Editor, args map[string]any) error {
		before := e.Active()
		var from int
		if before != nil {
			if err := before.RefuseInLargeMode(name); err != nil {
				return err
			}
			if handled, err := e.gateOnIndex(before, name, args); handled {
				return err
			}
			// A large document whose file changed under it is read-only, and revert is
			// how it is made usable again.
			if before.ReadOnly() && textChanging[name] && (name != "revert" || !before.Frozen()) {
				return ErrReadOnly
			}
			if textChanging[name] && name != "undo" && name != "redo" {
				before.LastMulti = nil
			}
			if textChanging[name] {
				// The search range holds positions, which an edit makes stale.
				before.FindRange = nil
			}
			from = before.Cursor.Line
		}
		err := fn(e, args)
		if d := e.Active(); d != nil {
			if d != before {
				from = d.Cursor.Line
			}
			d.RevealCarets(from, e.tabSizeOf(d))
		}
		return err
	}
}
