package syntax

// InlineMarkerKind is the kind of delimiter an InlineMarker describes.
type InlineMarkerKind uint8

// The inline delimiters the page view conceals.
const (
	MarkerEmphasis InlineMarkerKind = iota
	MarkerStrong
	MarkerCode
	MarkerLink
	MarkerStrike
)

// InlineMarker is one inline delimiter run of a Markdown line, as rune columns
// [Start, End) into the line: the "*"/"_" run of emphasis or strong, the backtick
// run of an inline code span, the brackets and destination of a link or image, or
// the "~~" run of a strike-through. The page view composes the line by hiding
// these runs (docs/markdown.md).
type InlineMarker struct {
	Start, End int
	Kind       InlineMarkerKind
}

// MarkdownInlineMarkers returns the inline delimiters of one line. It reuses the
// markdown inline reader (mdInline) rather than moving the parser: the reader
// records the byte ranges it treats as delimiters, and the ranges are converted to
// rune columns here.
func MarkdownInlineMarkers(line string) []InlineMarker {
	if line == "" {
		return nil
	}
	t := &mdTokenizer{text: []string{line}, raw: make([][]Span, 1)}
	p := mdInline{t: t, s: line}
	p.collect = true
	p.parse(0, len(line), KindPlain)
	if len(p.marks) == 0 {
		return nil
	}
	out := make([]InlineMarker, len(p.marks))
	for i, m := range p.marks {
		out[i] = InlineMarker{
			Start: runeColumn(line, m.start),
			End:   runeColumn(line, m.end),
			Kind:  InlineMarkerKind(m.kind),
		}
	}
	return out
}

// runeColumn converts a byte offset of s to a rune column.
func runeColumn(s string, byteOffset int) int {
	if byteOffset <= 0 {
		return 0
	}
	return len([]rune(s[:min(byteOffset, len(s))]))
}

// mark is a recorded delimiter run, in bytes of the line.
type mark struct {
	start, end int
	kind       InlineMarkerKind
}

// mark records a delimiter run of the line while the inline reader parses it.
func (p *mdInline) mark(from, to int, kind InlineMarkerKind) {
	if !p.collect || to <= from {
		return
	}
	p.marks = append(p.marks, mark{start: from, end: to, kind: kind})
}

// markCode records the backtick runs of an inline code span.
func (p *mdInline) markCode(i, n, j int) {
	p.mark(i, i+n, MarkerCode)
	p.mark(j, j+n, MarkerCode)
}

// markStrike records the tilde runs of a strike-through.
func (p *mdInline) markStrike(i, j int) {
	p.mark(i, i+2, MarkerStrike)
	p.mark(j, j+2, MarkerStrike)
}
