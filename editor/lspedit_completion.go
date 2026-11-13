package editor

// Multi-cursor snippet and auto-import completion acceptance.
//
// Fresh's insert_completion_text replaces each cursor's own word prefix with the expanded
// snippet text in one bulk edit and then walks every cursor to $0; its additional edits
// are applied separately. gvim keeps the same per-caret replacement and snippet placement
// but combines the auto-import edits with the inserts in ONE ApplyEditList, so a multi-
// caret snippet/auto-import accept is one undo step guarded by the request version. The
// combined list is validated for ambiguity first; an overlap refuses everything.

import (
	"fmt"
	"slices"

	"github.com/tbxark/gvim/buffer"
)

// ApplyCompletionItemAtCarets accepts a snippet/auto-import completion at EVERY caret of
// the open document at path in one atomic, version-guarded edit (one undo step): each
// caret replaces its own identifier prefix with text, the additional edits (the
// auto-import) are applied once, and for a snippet every caret is placed at the same rune
// offset inside its own inserted text. cursorOffset < 0 means no snippet placement (carets
// stay after their inserted text). Returns the number of carets; ErrStale leaves the
// document untouched, and ErrOverlappingEdits refuses an ambiguous combined list.
func (e *Editor) ApplyCompletionItemAtCarets(path, text string, cursorOffset int, additional []LSPEdit, wordChars string, expect *Version) (int, error) {
	d := e.lspDocAt(path)
	if d == nil {
		return 0, fmt.Errorf("%w: %s", ErrNotOpen, path)
	}
	edits := caretPrefixEdits(d, text, wordChars)
	carets := len(edits)
	edits = append(edits, additional...)
	if completionEditsConflict(d, edits) {
		return 0, ErrOverlappingEdits
	}
	if err := d.ApplyEditList(edits, EditOptions{Cursor: CursorMap, Expect: expect}, false); err != nil {
		return 0, err
	}
	if cursorOffset >= 0 {
		runes := len([]rune(text))
		back := runes - min(cursorOffset, runes)
		if back > 0 {
			// The primary caret and the extra carets are each after their own inserted
			// text (CursorMap); walking back by the same rune count puts them at the
			// snippet's final stop in their own insertion.
			d.Cursor = stepBackRunes(d.Buffer, d.Cursor, back)
			d.Sel = nil
			for i := range d.Extra {
				d.Extra[i].Cursor = stepBackRunes(d.Buffer, d.Extra[i].Cursor, back)
				d.Extra[i].Sel = nil
			}
		}
	}
	return carets, nil
}

// completionEditsConflict validates the combined per-caret inserts and additional edits
// before ApplyEditList: inverted ranges, overlapping ranges, an insert strictly inside a
// replaced range, and two zero-width inserts at the same point are all ambiguous and
// refuse the whole accept. Positions are clamped the way ApplyEditList clamps them.
func completionEditsConflict(d *Document, edits []LSPEdit) bool {
	clamped := make([]LSPEdit, len(edits))
	for i, ed := range edits {
		ed.From, ed.To = d.Buffer.Clamp(ed.From), d.Buffer.Clamp(ed.To)
		if ed.To.Less(ed.From) {
			return true
		}
		clamped[i] = ed
	}
	slices.SortStableFunc(clamped, func(a, b LSPEdit) int {
		if a.From != b.From {
			if a.From.Less(b.From) {
				return -1
			}
			return 1
		}
		if a.To != b.To {
			if a.To.Less(b.To) {
				return -1
			}
			return 1
		}
		return 0
	})
	for i := 1; i < len(clamped); i++ {
		prev, cur := clamped[i-1], clamped[i]
		if cur.From.Less(prev.To) {
			return true // overlaps, or an insert strictly inside the previous range
		}
		if prev.From == prev.To && cur.From == cur.To && prev.From == cur.From {
			return true // two inserts at one point: order would be ambiguous
		}
	}
	return false
}

// stepBackRunes moves p back n runes inside the document. A line break counts as one rune
// (there is no addressable newline position); it places snippet carets at their $0 stop
// inside the text just inserted at that caret.
func stepBackRunes(b *buffer.Buffer, p buffer.Pos, n int) buffer.Pos {
	p = b.Clamp(p)
	for n > 0 {
		if p.Col > 0 {
			line := []rune(b.Line(p.Line))
			step := min(n, min(p.Col, len(line)))
			p.Col -= step
			n -= step
			continue
		}
		if p.Line == 0 {
			break
		}
		p.Line--
		p.Col = len([]rune(b.Line(p.Line)))
		n--
	}
	return p
}
