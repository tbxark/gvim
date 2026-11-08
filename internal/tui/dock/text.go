package dock

import (
	"strings"
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
