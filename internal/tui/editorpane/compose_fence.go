package editorpane

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/syntax"
)

// The page view frames a fenced code block: the open delimiter becomes a top
// border with the language label, the body lines get side rails and padding
// while keeping their syntax colours, and the close delimiter becomes a bottom
// border. The frame is measure-4 cells wide (Fresh's codeFrameWidth), indented
// with the block, and the rails cost two cells each. A body line wider than the
// inner width wraps without rails on its continuation rows: that is a documented
// residual of the single-visual-row frame; see docs/markdown.md.

// frameWidth is the width of the frame of a fenced block indented by indent
// cells in a page of measure cells: Fresh's measure-4, shrunk by the indent.
func frameWidth(measure, indent int) int {
	return max(4, measure-indent-4)
}

// composeFenceLine composes one line of a fenced code block.
func (f frame) composeFenceLine(i int, fl syntax.FenceLine, raw []rune, kinds []syntax.Kind) composedLine {
	out := composedLine{}
	depth := max(fl.QuoteDepth, 0)
	base := quotePrefixLen(raw, depth)
	indent := min(max(fl.Indent, 0), max(len(raw)-base, 0))
	width := frameWidth(f.pageMeasure-2*depth, indent)
	src := min(base, len(raw))
	add := func(r rune, k syntax.Kind, s int) {
		out.runes = append(out.runes, r)
		out.kinds = append(out.kinds, k)
		out.src = append(out.src, s)
	}
	// The fence sits inside its blockquote: the container's bars come first, then
	// the frame at the block's own indent.
	for q := 0; q < depth; q++ {
		add('│', syntax.KindQuote, src)
		add(' ', syntax.KindQuote, src)
	}
	for j := 0; j < indent; j++ {
		if base+j < len(raw) {
			add(raw[base+j], kindAt(kinds, base+j), base+j)
		}
	}
	switch fl.Kind {
	case syntax.FenceOpen:
		f.composeFenceOpen(&out, fl, width, src, add)
	case syntax.FenceClose:
		add('╰', syntax.KindPlain, src)
		for j := 0; j < width-2; j++ {
			add('─', syntax.KindPlain, src)
		}
		add('╯', syntax.KindPlain, src)
	case syntax.FenceBody:
		inner := width - 4
		start := base + indent
		segments := fenceBodySegments(raw, start, inner)
		show := segments[0]
		if f.caret.Line == i {
			// The caret line keeps the whole body text (the wrapped rows without
			// rails are the editing view); the continuation rows stand down.
			show = nil
			for k := start; k < len(raw); k++ {
				r := raw[k]
				if r == '\t' {
					for q := 0; q < 4; q++ {
						show = append(show, fenceBodyRune{' ', k})
					}
					continue
				}
				if unicode.IsControl(r) {
					r = ' '
				}
				show = append(show, fenceBodyRune{r, k})
			}
		}
		add('│', syntax.KindPlain, src)
		add(' ', syntax.KindPlain, src)
		used := 0
		for _, fr := range show {
			add(fr.r, kindAt(kinds, fr.src), fr.src)
			used += ansi.StringWidth(string(fr.r))
		}
		for ; used < inner; used++ {
			add(' ', syntax.KindPlain, len(raw))
		}
		add(' ', syntax.KindPlain, len(raw))
		add('│', syntax.KindPlain, len(raw))
	}
	finishMaps(&out, len(raw))
	return out
}

// fenceBodyRune is one visible rune of a fence body line with its source column.
type fenceBodyRune struct {
	r   rune
	src int
}

// fenceBodySegments returns the visible runes of a body line wrapped at the
// frame's inner width: tabs expand to four blanks, control characters become
// blanks, and the rows break at the last space that fits (a word longer than the
// row is cut). The primary row shows segment 0; the caller's extra-row builder
// emits the rest as railed continuation rows.
func fenceBodySegments(raw []rune, start, inner int) [][]fenceBodyRune {
	inner = max(inner, 1)
	var content []fenceBodyRune
	for k := max(start, 0); k < len(raw); k++ {
		r := raw[k]
		if r == '\t' {
			for q := 0; q < 4; q++ {
				content = append(content, fenceBodyRune{' ', k})
			}
			continue
		}
		if unicode.IsControl(r) {
			r = ' '
		}
		content = append(content, fenceBodyRune{r, k})
	}
	var out [][]fenceBodyRune
	rowStart := 0
	for rowStart < len(content) {
		used := 0
		end := rowStart
		lastSpace := -1
		for end < len(content) {
			w := ansi.StringWidth(string(content[end].r))
			if used+w > inner && end > rowStart {
				switch {
				case content[end].r == ' ':
					out = append(out, content[rowStart:end])
					rowStart = end + 1
				case lastSpace > rowStart:
					out = append(out, content[rowStart:lastSpace])
					rowStart = lastSpace + 1
				default:
					out = append(out, content[rowStart:end])
					rowStart = end
				}
				for rowStart < len(content) && content[rowStart].r == ' ' {
					rowStart++
				}
				goto nextRow
			}
			used += w
			if content[end].r == ' ' {
				lastSpace = end
			}
			end++
		}
		out = append(out, content[rowStart:])
		break
	nextRow:
	}
	if len(out) == 0 {
		return [][]fenceBodyRune{{}}
	}
	return out
}

// fenceBodyRowText builds one railed row of a fence body from a segment: the
// indent, "| ", the segment padded to inner, " |".
func fenceBodyRowText(quoteDepth, indent, inner int, seg []fenceBodyRune) string {
	var b []rune
	for q := 0; q < quoteDepth; q++ {
		b = append(b, '│', ' ')
	}
	for j := 0; j < indent; j++ {
		b = append(b, ' ')
	}
	b = append(b, '\u2502', ' ')
	used := 0
	for _, fr := range seg {
		b = append(b, fr.r)
		used += ansi.StringWidth(string(fr.r))
	}
	for ; used < inner; used++ {
		b = append(b, ' ')
	}
	b = append(b, ' ', '\u2502')
	return string(b)
}

// composeFenceOpen composes the top border of a fenced block. The language label
// sits in the border when the frame is wide enough for it.
func (f frame) composeFenceOpen(out *composedLine, fl syntax.FenceLine, width, src int, add func(rune, syntax.Kind, int)) {
	label := fenceLabel(fl.Info)
	// "╭ " + label + " " + fill + "╮" needs room for the corners, the blanks and
	// at least one fill cell.
	if label != "" && width >= 4+ansiiWidth(label) {
		add('╭', syntax.KindPlain, src)
		add(' ', syntax.KindPlain, src)
		for _, r := range label {
			add(r, syntax.KindString, src)
		}
		add(' ', syntax.KindPlain, src)
		for j := 0; j < width-4-ansiiWidth(label); j++ {
			add('─', syntax.KindPlain, src)
		}
		add('╮', syntax.KindPlain, src)
		return
	}
	add('╭', syntax.KindPlain, src)
	for j := 0; j < width-2; j++ {
		add('─', syntax.KindPlain, src)
	}
	add('╮', syntax.KindPlain, src)
}

// fenceLabel is the language label of an info string, cut to a short word and
// stripped of control characters.
func fenceLabel(info string) string {
	word := strings.TrimSpace(info)
	if word == "" {
		return ""
	}
	word = strings.TrimLeft(word, "{.")
	if k := strings.IndexAny(word, " \t,{}"); k >= 0 {
		word = word[:k]
	}
	word = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || !unicode.IsGraphic(r) {
			return -1
		}
		return r
	}, word)
	if n := len([]rune(word)); n > 12 {
		word = string([]rune(word)[:12])
	}
	return word
}

// ansiiWidth is the display width of s, used for the label that sits in the top
// border.
func ansiiWidth(s string) int { return ansi.StringWidth(s) }

// quotePrefixLen is the number of source runes a blockquote prefix of depth
// levels takes: up to three blanks, then ">" with at most one blank after it per
// level.
func quotePrefixLen(raw []rune, depth int) int {
	i := 0
	for level := 0; level < depth; level++ {
		for i < len(raw) && raw[i] == ' ' && i < 3 {
			i++
		}
		if i >= len(raw) || raw[i] != '>' {
			return i
		}
		i++
		if i < len(raw) && raw[i] == ' ' {
			i++
		}
	}
	return i
}
