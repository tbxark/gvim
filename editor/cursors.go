package editor

import (
	"errors"
)

// undoAction reverts the newest undo group. When that group is the last
// multi-caret edit, the primary caret goes back to where it was before the edit.
// Otherwise the caret goes to the first edit of the group, as it always has.
func undoAction(_ *Editor, d *Document, _ map[string]any) error {
	m := d.LastMulti
	if m == nil || m.Undone {
		d.LastMulti = nil
		if pos, ok := d.Buffer.Undo(); ok {
			d.PlacePrimary(pos)
		}
		return nil
	}
	if _, ok := d.Buffer.Undo(); ok {
		m.Undone = true
		d.PlacePrimary(m.Before)
	}
	return nil
}

// redoAction reapplies the newest undone group. When that group is the last
// multi-caret edit, the primary caret goes to where it was after the edit.
// Otherwise the caret goes to the end of the group, as it always has.
func redoAction(_ *Editor, d *Document, _ map[string]any) error {
	m := d.LastMulti
	if m == nil || !m.Undone {
		d.LastMulti = nil
		if pos, ok := d.Buffer.Redo(); ok {
			d.PlacePrimary(pos)
		}
		return nil
	}
	if _, ok := d.Buffer.Redo(); ok {
		m.Undone = false
		d.PlacePrimary(m.After)
	}
	return nil
}

// typeAtCarets types the text at every caret, replacing each selection. It is
// the multi-caret form of insertText, with the smart rules of typeText.
func typeAtCarets(e *Editor, d *Document, args map[string]any) error {
	text, ok := args["text"].(string)
	if !ok {
		return errors.New("insert_char: missing string argument \"text\"")
	}
	e.typeText(d, text)
	return nil
}

// newlineAtCarets breaks the line at every caret, replacing each selection, with
// the indent rules of newline.
func newlineAtCarets(e *Editor, d *Document, _ map[string]any) error {
	e.newline(d)
	return nil
}

// deleteBackwardAtCarets removes each selection, or the character before each
// empty caret, with the smart rules of backspace.
func deleteBackwardAtCarets(e *Editor, d *Document, _ map[string]any) error {
	e.backspace(d)
	return nil
}

// deleteForwardAtCarets removes each selection, or the character after each
// empty caret, which may be a line break.
func deleteForwardAtCarets(_ *Editor, d *Document, _ map[string]any) error {
	d.EditAtCarets(func(c caret) edit {
		if lo, hi := c.Bounds(); lo != hi {
			return edit{From: lo, To: hi}
		}
		if next, ok := nextPos(d.Buffer, c.Head); ok {
			return edit{From: c.Head, To: next}
		}
		return edit{From: c.Head, To: c.Head}
	})
	return nil
}

// tabAtCarets handles Tab at every caret. If any caret has a selection, every
// line that a selection covers is indented, as Tab does for the primary caret.
// Otherwise each empty caret gets one indent unit: tab_size spaces, or a tab
// character when UseTabs is set.
func tabAtCarets(e *Editor, d *Document, _ map[string]any) error {
	if d.HasAnySelection() {
		d.IndentSelectedLines(e.indentUnit(d))
		return nil
	}
	unit := e.indentUnit(d)
	d.EditAtCarets(func(c caret) edit { return replaceEdit(c, unit) })
	return nil
}

// withCarets returns a handler that runs multi while the document has extra
// carets, and single otherwise. A single caret therefore behaves exactly as it
// did before the extra carets existed.
func withCarets(single handler, multi func(e *Editor, d *Document, args map[string]any) error) handler {
	return func(e *Editor, args map[string]any) error {
		if d := e.Active(); d != nil && len(d.Extra) > 0 {
			return multi(e, d, args)
		}
		return single(e, args)
	}
}

// collapseCarets returns a handler that drops the extra carets, then runs next.
// Undo and redo use it, because they cannot put the extra carets back.
func collapseCarets(next handler) handler {
	return func(e *Editor, args map[string]any) error {
		if d := e.Active(); d != nil {
			d.ClearCarets()
		}
		return next(e, args)
	}
}

// registerCursorHandlers binds the multi-caret actions. It runs last in
// buildHandlers, so it replaces the entries it names. The single-caret handlers
// it replaces for typing and deletion are kept inside withCarets, and run when
// there is one caret.
func registerCursorHandlers(h map[Action]handler) {
	h["insert_char"] = withCarets(h["insert_char"], typeAtCarets)
	h["insert_newline"] = withCarets(h["insert_newline"], newlineAtCarets)
	h["insert_tab"] = withCarets(h["insert_tab"], tabAtCarets)
	h["delete_backward"] = withCarets(h["delete_backward"], deleteBackwardAtCarets)
	h["delete_forward"] = withCarets(h["delete_forward"], deleteForwardAtCarets)
	registerMotionCaretHandlers(h)
	h["undo"] = collapseCarets(h["undo"])
	h["redo"] = collapseCarets(h["redo"])
	h["add_cursor_above"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		return d.AddCaretVertical(-1, e.tabSizeOf(d))
	})
	h["add_cursor_below"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		return d.AddCaretVertical(1, e.tabSizeOf(d))
	})
	h["add_cursor_next_match"] = docAction(func(_ *Editor, d *Document, _ map[string]any) error {
		return d.AddNextMatch()
	})
	h["add_cursors_to_line_ends"] = docAction(func(_ *Editor, d *Document, _ map[string]any) error {
		d.CursorsToLineEnds()
		return nil
	})
	// Escape sends this action. It drops the extra carets, the selection and mark
	// mode, as Fresh does, and reports nothing.
	h["remove_secondary_cursors"] = func(e *Editor, _ map[string]any) error {
		if d := e.Active(); d != nil {
			d.ClearCarets()
			d.Sel = nil
			delete(e.extras.marked, d)
		}
		return nil
	}
	h["block_select_up"] = blockHandler(-1, 0)
	h["block_select_down"] = blockHandler(1, 0)
	h["block_select_left"] = blockHandler(0, -1)
	h["block_select_right"] = blockHandler(0, 1)
}
