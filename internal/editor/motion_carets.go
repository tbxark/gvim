package editor

// Motions at every caret. A motion is written for the primary caret (Cursor, Sel
// and want of the Document). With extra carets, forEachCaret installs each caret
// as the primary one in turn, runs the step, and reads the result back, so the
// single-caret code is the only code. Each caret keeps its own wanted column.

// forEachCaret runs step once per caret, in document order. step sees the caret
// as d.Cursor, d.Sel and d.want, and primary says whether it is the real primary
// caret. Carets that meet afterwards are merged.
func (d *Document) forEachCaret(step func(primary bool)) {
	cs := d.allCarets()
	for i := range cs {
		c := &cs[i]
		d.Cursor, d.Sel, d.want = c.head, c.selection(), c.want-1
		step(c.primary)
		c.anchor, c.head, c.want = d.Cursor, d.Cursor, d.want+1
		if d.Sel != nil {
			c.anchor = d.Sel.Anchor
		}
	}
	d.setCarets(cs)
}

// moveCarets runs the motion m at every caret. extend grows each selection from
// its anchor; otherwise each caret moves and drops its selection.
func (e *Editor) moveCarets(d *Document, m motion, extend, vertical bool) {
	step := func(bool) {
		if !vertical {
			d.want = -1
		}
		d.move(m(e, d, extend), extend)
	}
	if len(d.extra) == 0 {
		step(true)
		return
	}
	d.forEachCaret(step)
}

// pageCarets moves the primary caret by a page, as pageMove does, and shifts
// every other caret by the same number of lines.
func (e *Editor) pageCarets(d *Document, dir int, extend bool) {
	from := d.Cursor.Line
	pageMove(e, d, dir, extend)
	if len(d.extra) == 0 {
		return
	}
	delta := d.Cursor.Line - from
	d.forEachCaret(func(primary bool) {
		if !primary {
			d.move(e.verticalTarget(d, d.Cursor, d.Cursor.Line+delta), extend)
		}
	})
}

// atEveryCaret wraps a handler that works on the primary caret, so that it runs
// at every caret when there are extra ones. It returns the first error.
func atEveryCaret(single handler) handler {
	return withCarets(single, func(e *Editor, d *Document, args map[string]any) error {
		var first error
		d.forEachCaret(func(bool) {
			if err := single(e, args); err != nil && first == nil {
				first = err
			}
		})
		return first
	})
}

// registerMotionCaretHandlers makes the selection and paragraph actions that
// have no motion form work at every caret.
func registerMotionCaretHandlers(h map[Action]handler) {
	for _, a := range []Action{
		"select_word",
		"move_to_paragraph_up", "select_to_paragraph_up",
		"move_to_paragraph_down", "select_to_paragraph_down",
	} {
		h[a] = atEveryCaret(h[a])
	}
}
