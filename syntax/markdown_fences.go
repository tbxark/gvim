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
	// QuoteDepth is the number of blockquote levels the fence sits in: the fence
	// lines carry that many ">" markers before the fence run, and the page view
	// frames the block after that many quote bars. Zero is a top-level fence.
	QuoteDepth int
}

// MarkdownFences classifies the lines of text by the fenced code blocks the
// markdown tokenizer would read: an open delimiter line, its body lines, and the
// closing delimiter (or the end of the document when it has none). The rules are
// the tokenizer's (mdFence, closesFence); it is a separate pass so the page view
// can frame a block without moving the parser.
func MarkdownFences(lines []string) []FenceLine {
	out := make([]FenceLine, len(lines))
	for i := 0; i < len(lines); {
		depth, content, ok := fenceQuotePrefix(strings.TrimRight(lines[i], "\r"))
		if !ok {
			i++
			continue
		}
		m := mdFence.FindStringSubmatch(content)
		if m == nil {
			i++
			continue
		}
		marker := m[2]
		out[i] = FenceLine{Kind: FenceOpen, Indent: len(m[1]), Marker: marker,
			Info: strings.TrimSpace(m[3]), QuoteDepth: depth}
		i++
		for ; i < len(lines); i++ {
			line := strings.TrimRight(lines[i], "\r")
			body, ok := stripQuotePrefix(line, depth)
			if !ok {
				// The container ended: the fence has no close line, and the lines
				// outside the container are classified on their own (literal fallback).
				break
			}
			if closesFence(body, marker) {
				out[i] = FenceLine{Kind: FenceClose, Indent: leadingSpaces(body), Marker: marker, QuoteDepth: depth}
				i++
				break
			}
			out[i] = FenceLine{Kind: FenceBody, Indent: leadingSpaces(body), QuoteDepth: depth}
		}
	}
	return out
}

// fenceQuotePrefix strips the blockquote markers of s: up to three blanks, then
// repeated ">" with at most one blank after each. It returns the depth, the line
// without the prefix, and false when the first marker is not a quote marker.
func fenceQuotePrefix(s string) (depth int, rest string, ok bool) {
	rest = s
	for depth < 8 {
		trimmed := strings.TrimLeft(rest, " ")
		if len(rest)-len(trimmed) > 3 || !strings.HasPrefix(trimmed, ">") {
			if depth > 0 {
				return depth, rest, true
			}
			return depth, rest, true
		}
		rest = strings.TrimPrefix(trimmed[1:], " ")
		depth++
	}
	return depth, rest, true
}

// stripQuotePrefix removes want blockquote markers from s, or reports false when
// the line does not carry them.
func stripQuotePrefix(s string, want int) (string, bool) {
	rest := s
	for i := 0; i < want; i++ {
		trimmed := strings.TrimLeft(rest, " ")
		if len(rest)-len(trimmed) > 3 || !strings.HasPrefix(trimmed, ">") {
			return "", false
		}
		rest = strings.TrimPrefix(trimmed[1:], " ")
	}
	return rest, true
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
