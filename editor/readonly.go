package editor

import (
	"errors"
	"fmt"
)

// ErrReadOnly is returned by an action that would change a read-only document,
// and by Save, which would change its file. The status shows "read-only".
var ErrReadOnly = errors.New("read-only")

// textChanging lists the actions that change the text of a document. A read-only
// document refuses each of them with ErrReadOnly before the action runs. Save is
// refused by Editor.Save, and save_as is allowed, because it writes a new file.
// TestEveryActionLeavesReadOnlyTextAlone fails for a text-changing action that is
// missing from this list.
var textChanging = map[Action]bool{
	"insert_char":              true,
	"insert_newline":           true,
	"insert_tab":               true,
	"dedent_selection":         true,
	"duplicate_line":           true,
	"delete_line":              true,
	"open_line":                true,
	"to_upper_case":            true,
	"to_lower_case":            true,
	"transpose_chars":          true,
	"toggle_case":              true,
	"toggle_comment":           true,
	"delete_backward":          true,
	"delete_forward":           true,
	"delete_word_backward":     true,
	"delete_word_forward":      true,
	"delete_to_line_start":     true,
	"delete_to_line_end":       true,
	"move_line_up":             true,
	"move_line_down":           true,
	"cut":                      true,
	"paste":                    true,
	"undo":                     true,
	"redo":                     true,
	"reload_with_encoding":     true,
	"dabbrev_expand":           true,
	"shell_command_replace":    true,
	"sort_lines":               true,
	"duplicate_line_above":     true,
	"trim_trailing_whitespace": true,
	"ensure_final_newline":     true,
	"revert":                   true,
	"format_buffer":            true,
}

// ReadOnly reports whether the document refuses changes to its text and to its
// file. Movement, selection, copy, save_as and the encoding actions still work.
func (d *Document) ReadOnly() bool {
	return d.readOnly
}

// toggleReadOnly turns the read-only flag of the document on or off.
func toggleReadOnly(e *Editor, d *Document, _ map[string]any) error {
	if d.binary {
		return fmt.Errorf("toggle_read_only: %w", ErrBinary)
	}
	d.readOnly = !d.readOnly
	if d.readOnly {
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
			if err := before.refuseInLargeMode(name); err != nil {
				return err
			}
			if handled, err := e.gateOnIndex(before, name, args); handled {
				return err
			}
			// A large document whose file changed under it is read-only, and revert is
			// how it is made usable again.
			if before.readOnly && textChanging[name] && (name != "revert" || !before.frozen()) {
				return ErrReadOnly
			}
			if textChanging[name] && name != "undo" && name != "redo" {
				before.lastMulti = nil
			}
			if textChanging[name] {
				// The search range holds positions, which an edit makes stale.
				before.findRange = nil
			}
			from = before.Cursor.Line
		}
		err := fn(e, args)
		if d := e.Active(); d != nil {
			if d != before {
				from = d.Cursor.Line
			}
			d.revealCarets(from, e.tabSizeOf(d))
		}
		return err
	}
}
