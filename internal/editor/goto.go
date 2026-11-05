package editor

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/tbxark/gvim/internal/buffer"
)

// ErrBadLine is returned by GotoLine when the line is below 1.
var ErrBadLine = errors.New("line numbers start at 1")

// GotoLine moves the caret of the active document to a 1-based line and column,
// clears any selection, and scrolls the viewport so the caret is on screen. A
// line past the end goes to the last line. A column past the end of its line
// goes to the end of that line. A column below 1 goes to the first column. A
// line below 1 moves nothing and returns an error wrapping ErrBadLine.
//
// GotoLine is the target of the go-to-line prompt. The goto_line action does not
// use it: Ctrl+G in the default keymap only reports the find message.
func (e *Editor) GotoLine(line, col int) error {
	if e.Empty() {
		err := fmt.Errorf("goto line %d: %w", line, ErrNoDocument)
		e.setStatus(err.Error())
		return err
	}
	if line < 1 {
		err := fmt.Errorf("goto line %d: %w", line, ErrBadLine)
		e.setStatus(err.Error())
		return err
	}
	d := e.Active()
	target := buffer.Pos{Line: min(line, d.Buffer.Lines()) - 1, Col: max(col, 1) - 1}
	d.want = -1
	d.move(d.Buffer.Clamp(target), false)
	e.ensureVisible(d)
	return nil
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
