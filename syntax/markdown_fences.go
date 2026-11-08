package syntax

import "strings"

// FenceKind classifies a line of a Markdown document by its place in a fenced
// code block. The page view frames a block: the open line becomes a top border
// with the language label, the body lines get side rails, and the close line a
// bottom border. See docs/markdown.md.
type FenceKind uint8

// The fence line kinds. A line outside every fence is FenceNone.
const (
	FenceNone FenceKind = iota
	FenceOpen
	FenceBody
	FenceClose
)

// FenceLine is the fenced-code classification of one line.
type FenceLine struct {
	Kind FenceKind
	// Indent is the number of leading blanks of the fence delimiter, or of the
	// body line, in runes.
	Indent int
	// Marker is the run of backticks or tildes of the delimiter (the whole run
	// for an open line, the closing run for a close line).
	Marker string
	// Info is the info string of an open line, trimmed ("go", "{.python}").
	Info string
}

// MarkdownFences classifies the lines of text by the fenced code blocks the
// markdown tokenizer would read: an open delimiter line, its body lines, and the
// closing delimiter (or the end of the document when it has none). The rules are
// the tokenizer's (mdFence, closesFence); it is a separate pass so the page view
// can frame a block without moving the parser.
func MarkdownFences(lines []string) []FenceLine {
	out := make([]FenceLine, len(lines))
	for i := 0; i < len(lines); {
		s := strings.TrimRight(lines[i], "\r")
		m := mdFence.FindStringSubmatch(s)
		if m == nil {
			i++
			continue
		}
		marker := m[2]
		out[i] = FenceLine{Kind: FenceOpen, Indent: len(m[1]), Marker: marker, Info: strings.TrimSpace(m[3])}
		i++
		for ; i < len(lines); i++ {
			body := strings.TrimRight(lines[i], "\r")
			if closesFence(body, marker) {
				out[i] = FenceLine{Kind: FenceClose, Indent: leadingSpaces(body), Marker: marker}
				i++
				break
			}
			out[i] = FenceLine{Kind: FenceBody, Indent: leadingSpaces(body)}
		}
	}
	return out
}

// leadingSpaces is the number of leading blank runes of s.
func leadingSpaces(s string) int {
	n := 0
	for _, r := range s {
		if r != ' ' && r != '\t' {
			break
		}
		n++
	}
	return n
}
