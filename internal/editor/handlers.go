package editor

import (
	"errors"
	"fmt"

	"github.com/tbxark/gvim/internal/buffer"
)

// handler runs one action against the editor.
type handler func(e *Editor, args map[string]any) error

// moveSpec pairs a cursor motion with its plain and selecting action names.
type moveSpec struct {
	move, sel Action
	motion    motion
	// vertical moves keep the wanted column; all others reset it.
	vertical bool
}

// moves lists every cursor motion the keymap uses. Each one is registered
// twice: once to move, and once to extend the selection.
var moves = []moveSpec{
	{"move_left", "select_left", motionLeft, false},
	{"move_right", "select_right", motionRight, false},
	{"move_up", "select_up", motionUp, true},
	{"move_down", "select_down", motionDown, true},
	{"move_word_left", "select_word_left", motionWordLeft, false},
	{"move_word_right", "select_word_right", motionWordRight, false},
	{"move_word_end", "select_word_end", motionWordEnd, false},
	{"move_line_start", "select_line_start", motionLineStart, false},
	{"move_line_end", "select_line_end", motionLineEnd, false},
	{"smart_home", "select_smart_home", motionSmartHome, false},
	{"move_document_start", "select_document_start", motionDocStart, false},
	{"move_document_end", "select_document_end", motionDocEnd, false},
}

// handlers maps every action name to its implementation.
var handlers = buildHandlers()

func buildHandlers() map[Action]handler {
	h := make(map[Action]handler, 2*len(moves)+32)
	for _, m := range moves {
		h[m.move] = moveHandler(m.motion, false, m.vertical)
		h[m.sel] = moveHandler(m.motion, true, m.vertical)
	}
	h["move_page_up"] = pageHandler(-1, false)
	h["select_page_up"] = pageHandler(-1, true)
	h["move_page_down"] = pageHandler(1, false)
	h["select_page_down"] = pageHandler(1, true)
	h["select_word"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		d.selectWord()
		return nil
	})
	h["select_all"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		d.selectAll()
		return nil
	})
	h["insert_char"] = docAction(insertChar)
	h["insert_newline"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		d.insertText("\n")
		return nil
	})
	h["insert_tab"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		d.insertText("\t")
		return nil
	})
	h["delete_backward"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		d.deleteBackward()
		return nil
	})
	h["delete_forward"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		d.deleteForward()
		return nil
	})
	h["delete_word_backward"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		d.deleteTo(wordLeft(d.Buffer, d.Cursor))
		return nil
	})
	h["delete_word_forward"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		d.deleteTo(wordRight(d.Buffer, d.Cursor))
		return nil
	})
	h["delete_to_line_start"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		if d.Cursor.Col == 0 {
			d.deleteBackward()
			return nil
		}
		d.deleteTo(buffer.Pos{Line: d.Cursor.Line})
		return nil
	})
	h["delete_to_line_end"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		end := buffer.Pos{Line: d.Cursor.Line, Col: d.Buffer.LineLen(d.Cursor.Line)}
		if d.Cursor == end {
			d.deleteForward()
			return nil
		}
		d.deleteTo(end)
		return nil
	})
	h["move_line_up"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		d.moveLine(-1)
		return nil
	})
	h["move_line_down"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		d.moveLine(1)
		return nil
	})
	h["copy"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		text, err := d.copySelection()
		if err != nil {
			return err
		}
		e.clipboard = text
		return nil
	})
	h["cut"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		text, err := d.copySelection()
		if err != nil {
			return err
		}
		e.clipboard = text
		d.deleteSelection()
		return nil
	})
	h["paste"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		if e.clipboard != "" {
			d.insertText(e.clipboard)
			e.setStatus("Pasted")
		}
		return nil
	})
	h["undo"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		if pos, ok := d.Buffer.Undo(); ok {
			d.Cursor = d.Buffer.Clamp(pos)
			d.Sel = nil
			d.want = -1
		}
		return nil
	})
	h["redo"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		if pos, ok := d.Buffer.Redo(); ok {
			d.Cursor = d.Buffer.Clamp(pos)
			d.Sel = nil
			d.want = -1
		}
		return nil
	})
	// save returns ErrNoPath for an untitled buffer. The UI then runs Save As.
	h["save"] = func(e *Editor, _ map[string]any) error {
		return e.Save()
	}
	h["save_as"] = func(e *Editor, args map[string]any) error {
		path, ok := args["path"].(string)
		if !ok {
			return errors.New("save_as: missing string argument \"path\"")
		}
		return e.saveAs(path, forced(args))
	}
	// The editor has one cursor, so there are no secondary cursors to remove.
	// Escape sends this action, and it must do nothing and report nothing.
	h["remove_secondary_cursors"] = func(*Editor, map[string]any) error {
		return nil
	}
	// new opens Untitled-N in a new tab. The untouched placeholder buffer is
	// replaced instead, so the first New in a session does not leave "[No Name]".
	h["new"] = func(e *Editor, _ map[string]any) error {
		d := e.newUntitled()
		if e.pristineUntitled() && e.docs[0].untitled == 0 {
			e.docs[0] = d
			e.switchTo(0)
		} else {
			e.docs = append(e.docs, d)
			e.switchTo(len(e.docs) - 1)
		}
		e.setStatus("New buffer")
		return nil
	}
	h["goto_line"] = func(e *Editor, _ map[string]any) error {
		// The goto_line action reports this message, and the legacy screen S24
		// requires it. The legacy macOS keymap, which the legacy screens were captured
		// with, binds Ctrl+G to find_next, and find_next reports this message when
		// no search is active. The go-to-line prompt is opened from the palette
		// and the Go menu, and it calls GotoLine.
		e.setStatus("No active search. Press Ctrl+F to search.")
		return nil
	}
	h["close_tab"] = closeTab
	h["next_buffer"] = func(e *Editor, _ map[string]any) error {
		if len(e.docs) > 0 {
			e.switchTo((e.active + 1) % len(e.docs))
		}
		return nil
	}
	h["prev_buffer"] = func(e *Editor, _ map[string]any) error {
		if len(e.docs) > 0 {
			e.switchTo((e.active - 1 + len(e.docs)) % len(e.docs))
		}
		return nil
	}
	// quit refuses while a tab has unsaved edits, unless force is set. The UI
	// uses Editor.Quit to get the list of dirty tabs instead.
	h["quit"] = func(e *Editor, args map[string]any) error {
		if !forced(args) {
			if err := unsavedError(e.docs); err != nil {
				return err
			}
		}
		e.quit = true
		return nil
	}
	return h
}

// docAction adapts an action that works on the active document into a handler.
func docAction(fn func(e *Editor, d *Document, args map[string]any) error) handler {
	return func(e *Editor, args map[string]any) error {
		return fn(e, e.Active(), args)
	}
}

// pageHandler moves the cursor one pane in direction dir (-1 up, +1 down).
func pageHandler(dir int, extend bool) handler {
	return docAction(func(e *Editor, d *Document, _ map[string]any) error {
		pageMove(e, d, dir, extend)
		return nil
	})
}

// pageOverlap is how many rows of the old page stay visible after a page move.
const pageOverlap = 3

// scrollOff is the margin the caret keeps from the top and bottom of the pane.
const scrollOff = 3

// pageMove scrolls the viewport by one page minus the overlap, then places the
// caret on the same screen row it had before, clamped into the scroll-off margin.
// The top is clamped to the document, which can leave the caret short of the
// full page at the end. If the viewport cannot scroll, the caret moves by the
// same amount instead.
func pageMove(e *Editor, d *Document, dir int, extend bool) {
	rows := e.rows
	lines := d.Buffer.Lines()
	delta := max(rows-min(pageOverlap, rows-1), 1) * dir

	oldTop := d.Top
	newTop := min(max(oldTop+delta, 0), max(lines-rows, 0))
	if newTop == oldTop {
		line := min(max(d.Cursor.Line+delta, 0), lines-1)
		d.move(verticalTarget(d, line), extend)
		return
	}

	margin := min(scrollOff, (rows-1)/2)
	row := d.Cursor.Line - oldTop
	if row < 0 || row >= rows {
		row = margin // an off-screen caret lands at the margin
	}
	row = min(max(row, margin), rows-1-margin)
	line := min(newTop+row, lines-1)
	d.Top = newTop
	d.move(verticalTarget(d, line), extend)
}

// moveHandler runs a cursor motion, optionally extending the selection.
func moveHandler(m motion, extend, vertical bool) handler {
	return docAction(func(e *Editor, d *Document, _ map[string]any) error {
		if !vertical {
			d.want = -1
		}
		d.move(m(d, e.rows), extend)
		return nil
	})
}

func insertChar(_ *Editor, d *Document, args map[string]any) error {
	text, ok := args["text"].(string)
	if !ok {
		return errors.New("insert_char: missing string argument \"text\"")
	}
	d.insertText(text)
	return nil
}

// closeTab closes the active tab. A modified buffer is kept unless force is set.
// With no tab open there is nothing to close.
func closeTab(e *Editor, args map[string]any) error {
	if e.Empty() {
		return ErrNoDocument
	}
	return e.closeAt(e.active, forced(args))
}

// forced reads the optional "force" argument.
func forced(args map[string]any) bool {
	force, _ := args["force"].(bool)
	return force
}

// unsavedError names the first modified document, or returns nil if none is.
func unsavedError(docs []*Document) error {
	for _, d := range docs {
		if d.Modified() {
			return fmt.Errorf("%s: %w", d.Name(), ErrUnsaved)
		}
	}
	return nil
}
