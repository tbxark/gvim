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
	indent := min(max(fl.Indent, 0), len(raw))
	for j := 0; j < indent; j++ {
		out.runes = append(out.runes, raw[j])
		out.kinds = append(out.kinds, kindAt(kinds, j))
		out.src = append(out.src, j)
	}
	width := frameWidth(f.pageMeasure, indent)
	src := min(indent, len(raw))
	add := func(r rune, k syntax.Kind, s int) {
		out.runes = append(out.runes, r)
		out.kinds = append(out.kinds, k)
		out.src = append(out.src, s)
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
		add('│', syntax.KindPlain, src)
		add(' ', syntax.KindPlain, src)
		used := 0
		for k := indent; k < len(raw); k++ {
			r := raw[k]
			if r == '\t' {
				for q := 0; q < 4; q++ {
					add(' ', kindAt(kinds, k), indent+k)
				}
				used += 4
				continue
			}
			if unicode.IsControl(r) {
				r = ' '
			}
			add(r, kindAt(kinds, k), indent+k)
			used += ansi.StringWidth(string(r))
		}
		// The padding keeps the frame's right rail in its column; a body line
		// wider than the inner width overflows and its continuation rows have no
		// rails (documented residual).
		for ; used < inner; used++ {
			add(' ', syntax.KindPlain, len(raw))
		}
		add(' ', syntax.KindPlain, len(raw))
		add('│', syntax.KindPlain, len(raw))
	}
	finishMaps(&out, len(raw))
	return out
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
