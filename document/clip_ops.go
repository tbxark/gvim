package document

import (
	"fmt"
	"github.com/tbxark/gvim/buffer"
	"slices"
	"strings"
)

// normalizePaste turns CRLF and CR into LF. The buffer is always LF in memory,
// and the line ending is applied on save.
func normalizePaste(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// copyText returns the text a copy or cut takes, and whether it is whole lines.
// The selections of all carets are joined with a newline, and a block selection
// keeps an empty row for a line the rectangle misses. With no selection the text
// is the line of each caret, once, with the break after the last one unless it is
// the last line of the document. The text is empty when there is nothing to take.
// A large document refuses a copy of more than 16 MiB, or of a line that is cut
// short, with an error and no text, so the clipboard never holds less than was
// selected.
func (d *Document) CopyText() (text string, lines bool, err error) {
	// A virtual block selection copies a true rectangle: the part of each line
	// inside the rectangle, padded with spaces when the line ends before the
	// rectangle's right edge. A zero-width rectangle selects no text (a bare
	// vertical column of carets), as Fresh's does.
	if rect, ok := d.BlockSelection(); ok && rect.Virtual {
		if rect.Width() == 0 {
			return "", false, nil
		}
		return d.copyVirtualBlockText(rect)
	}
	cs := d.AllCarets()
	if slices.ContainsFunc(cs, func(c caret) bool { lo, hi := c.Bounds(); return lo != hi }) {
		block := d.blockInProgress()
		parts := make([]string, 0, len(cs))
		total := 0
		for _, c := range cs {
			lo, hi := c.Bounds()
			if lo != hi || block {
				part, err := d.ReadRange(lo, hi)
				if err != nil {
					return "", false, err
				}
				if total += len(part); d.large != nil && total > buffer.MaxMaterialize {
					return "", false, errSelectionTooLarge()
				}
				parts = append(parts, part)
			}
		}
		return strings.Join(parts, "\n"), false, nil
	}
	var parts []string
	prev := -1
	for _, c := range cs {
		if c.Head.Line != prev {
			prev = c.Head.Line
			if err := d.checkLines(prev, prev, "copied"); err != nil {
				return "", false, err
			}
			parts = append(parts, d.Buffer.Line(prev))
		}
	}
	text = strings.Join(parts, "\n")
	if prev < d.Buffer.Lines()-1 {
		text += "\n"
	}
	return text, true, nil
}

// copyVirtualBlockText returns the text of the rectangle rect: for each line, its
// runes in the rectangle's columns, padded with spaces to the rectangle's width
// when the line is shorter. A line that ends left of the rectangle contributes
// only spaces.
func (d *Document) copyVirtualBlockText(rect BlockRect) (string, bool, error) {
	parts := make([]string, 0, rect.Lines())
	total, width := 0, rect.Width()
	for line := rect.Top; line <= rect.Bottom; line++ {
		if err := d.checkLines(line, line, "copied"); err != nil {
			return "", false, err
		}
		runes := []rune(d.Buffer.Line(line))
		from, to := min(rect.Left, len(runes)), min(rect.Right, len(runes))
		part := string(runes[from:to])
		if have := to - from; have < width {
			part += strings.Repeat(" ", width-have)
		}
		if total += len(part); d.large != nil && total > buffer.MaxMaterialize {
			return "", false, errSelectionTooLarge()
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "\n"), false, nil
}

// cutLines deletes the line of each caret, with its break, as one undo step. The
// last line of the document loses its text only. Carets on one line delete it once.
func (d *Document) CutLines() {
	prev := -1
	var prevTo buffer.Pos
	d.EditAtCarets(func(c caret) edit {
		line := c.Head.Line
		if line == prev {
			return edit{From: prevTo, To: prevTo}
		}
		prev = line
		prevTo = buffer.Pos{Line: line + 1}
		if line+1 >= d.Buffer.Lines() {
			prevTo = buffer.Pos{Line: line, Col: d.Buffer.LineLen(line)}
		}
		return edit{From: buffer.Pos{Line: line}, To: prevTo}
	})
}

// pasteText inserts text at every caret, replacing the selections, as one undo
// step. When the text has as many lines as there are carets, and more than one,
// caret i gets line i.
func (d *Document) PasteText(text string) {
	d.MaterializeVirtual()
	lines := strings.Split(text, "\n")
	if len(lines) > 1 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	column := len(lines) > 1 && len(lines) == len(d.AllCarets())
	i := 0
	d.EditAtCarets(func(c caret) edit {
		if !column {
			return replaceEdit(c, text)
		}
		i++
		return replaceEdit(c, lines[i-1])
	})
}

// ErrPasteTooLarge is the refusal of a paste of more than buffer.MaxMaterialize bytes.
// The paste runs on the UI goroutine, and copies and splits the text several times,
// so a paste of hundreds of megabytes would freeze the editor and exhaust memory.
var ErrPasteTooLarge = fmt.Errorf("paste too large (limit %d MiB)", buffer.MaxMaterialize>>20)
