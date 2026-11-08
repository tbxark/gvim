package tui

import (
	"strings"

	"github.com/tbxark/gvim/buffer"
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
	text, _ := textBetweenWithin(doc, a, b, 0, 0)
	return text
}

// textBetweenWithin is textBetween that gives up, and reports false, when the text is
// more than maxBytes bytes or spans more than maxLines lines. A zero limit is no limit.
// It reads no more than the limits allow, so a selection of a large document is
// refused after a bounded amount of work, not after reading all of it.
func textBetweenWithin(doc Document, a, b buffer.Pos, maxBytes, maxLines int) (string, bool) {
	if b.Less(a) {
		a, b = b, a
	}
	var sb strings.Builder
	for line := a.Line; line <= b.Line && line < doc.LineCount(); line++ {
		if maxLines > 0 && line-a.Line >= maxLines {
			return "", false
		}
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
		if maxBytes > 0 && sb.Len() > maxBytes {
			return "", false
		}
	}
	return sb.String(), true
}

// The most that the send of a large document's selection to the shell takes: a
// selection of a large document can span the whole file.
const (
	maxSendBytes = buffer.MaxMaterialize
	maxSendLines = 200_000
)

// sentText is the text sendSelection sends. It is selectedText, except that the
// selection of a large document over maxSendBytes bytes or maxSendLines lines is
// refused, which ok reports.
func sentText(doc Document) (text string, ok bool) {
	if !isLargeDoc(doc) {
		return selectedText(doc), true
	}
	if anchor, head, sel := doc.Selection(); sel && anchor != head {
		return textBetweenWithin(doc, anchor, head, maxSendBytes, maxSendLines)
	}
	return selectedText(doc), true
}
