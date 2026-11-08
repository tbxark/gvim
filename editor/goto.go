package editor

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/tbxark/gvim/buffer"
)

// ErrBadLine is returned by GotoLine when the line is below 1.
var ErrBadLine = errors.New("line numbers start at 1")

// GotoLine moves the caret of the active document to a 1-based line and column,
// clears any selection, and scrolls the viewport so the caret is on screen. A
// line past the end goes to the last line. A column past the end of its line
// goes to the end of that line. A column below 1 goes to the first column. A
// line below 1 moves nothing and returns an error wrapping ErrBadLine.
//
// In a large document whose line index is incomplete, a line the index has not
// reached waits: the status shows the progress of the scan, and the caret moves when
// the index gets to the line (see pending.go). A later GotoLine replaces the wait.
//
// GotoLine is the jump of LSP results, session restore and file:line arguments. The
// goto_line action does not use it: Ctrl+G in the default keymap only reports the find
// message.
func (e *Editor) GotoLine(line, col int) error {
	return e.gotoLine(line, col, false)
}

// GotoLineSelecting is GotoLine for the go-to-line prompt. In mark mode (set_mark) it
// extends the selection from its anchor, as every plain move does, so "set mark, then go
// to line" selects up to that line. Without a mark it is GotoLine.
func (e *Editor) GotoLineSelecting(line, col int) error {
	return e.gotoLine(line, col, true)
}

func (e *Editor) gotoLine(line, col int, selecting bool) error {
	if e.Empty() {
		err := fmt.Errorf("goto line %d: %w", line, ErrNoDocument)
		e.setError(err)
		return err
	}
	if line < 1 {
		err := fmt.Errorf("goto line %d: %w", line, ErrBadLine)
		e.setError(err)
		return err
	}
	d := e.Active()
	if d.LargeState() != nil {
		// A new goto replaces the one that waits for the index.
		d.LargeState().Pending = nil
		if info := d.Index(); line > info.Lines && !info.Complete {
			d.LargeState().Pending = &pending{Kind: pendingGoto, Line: line, Col: col, Selecting: selecting, From: jumpEntryOf(d)}
			e.setStatus(scanningStatus(info))
			return nil
		}
	}
	e.pushJump()
	e.gotoIn(d, line, col, selecting)
	return nil
}

// gotoIn moves the caret of d to a 1-based line and column, as GotoLine does, with the
// line and column clamped into the text. d need not be the active document. With
// selecting set and mark mode on, the move extends the selection from its anchor.
func (e *Editor) gotoIn(d *Document, line, col int, selecting bool) {
	target := buffer.Pos{Line: min(line, d.Buffer.Lines()) - 1, Col: max(col, 1) - 1}
	d.Want = -1
	d.Move(d.Buffer.Clamp(target), selecting && e.marking(d))
	e.ensureVisibleForJump(d)
}

// ParseLineCol reads the input of the go-to-line prompt: "N" for a line, or
// "N:COL" for a line and a column. Both numbers are 1-based and must be at least
// 1. Spaces around the input and around each number are ignored. When COL is
// omitted it is 1. ok is false for an empty input, a zero or negative number, a
// sign, a non-numeric part, or more than two parts.
func ParseLineCol(s string) (line, col int, ok bool) {
	lineText, colText, hasCol := strings.Cut(strings.TrimSpace(s), ":")
	line, ok = parsePosition(lineText)
	if !ok {
		return 0, 0, false
	}
	if !hasCol {
		return line, 1, true
	}
	col, ok = parsePosition(colText)
	if !ok {
		return 0, 0, false
	}
	return line, col, true
}

// parsePosition reads one unsigned decimal number of at least 1.
func parsePosition(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}
