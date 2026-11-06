package ui

import (
	"strings"

	"github.com/tbxark/gvim/internal/buffer"
)

// lineBreakReplacer turns CRLF and CR into LF, the line break a shell takes as Enter.
var lineBreakReplacer = strings.NewReplacer("\r\n", "\n", "\r", "\n")

// lineBreaks returns s with every line break as LF.
func lineBreaks(s string) string {
	return lineBreakReplacer.Replace(s)
}

// pasteText is the text a paste or a send writes to the shell: line breaks as LF, and
// no ESC, so the text cannot carry a control sequence or end a bracketed paste early.
func pasteText(s string) string {
	return strings.ReplaceAll(lineBreaks(s), "\x1b", "")
}

// textBetween returns the text from a to b, in document order, with a line break
// between lines. Columns count runes, and a column past the end of its line is the end
// of the line.
func textBetween(doc Document, a, b buffer.Pos) string {
	if b.Less(a) {
		a, b = b, a
	}
	var sb strings.Builder
	for line := a.Line; line <= b.Line && line < doc.LineCount(); line++ {
		runes := []rune(doc.Line(line))
		start, end := 0, len(runes)
		if line == a.Line {
			start = min(a.Col, len(runes))
		}
		if line == b.Line {
			end = min(b.Col, len(runes))
		}
		sb.WriteString(string(runes[start:end]))
		if line != b.Line {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}
