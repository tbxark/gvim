package editorpane

import (
	"image/color"
	"unicode"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/textlayout"
)

// markOccurrences sets the occurrence mark of each whole-word match of the word at
// the caret, or of each match of the selected text. It is set on the cells of line
// i, which the search matches then override. The selection itself is left to the
// selection color.
func (f frame) markOccurrences(i int, raw []rune, cells []cell) {
	if f.styles.Occurrence.BG == nil {
		return
	}
	var spans []textlayout.Span
	switch fx := f.fx; {
	case fx.occSel != "":
		for _, col := range textlayout.Matches(string(raw), fx.occSel) {
			if i == fx.occLine && col+f.off == fx.occFrom {
				continue
			}
			spans = append(spans, textlayout.Span{Start: col, End: col + utf8.RuneCountInString(fx.occSel)})
		}
	case f.occWord != "":
		spans = textlayout.Occurrences([]string{string(raw)}, f.occWord)
	}
	for _, s := range spans {
		from, to := expandedIndex(raw, s.Start, f.tab), expandedIndex(raw, s.End, f.tab)
		for k := from; k < to && k < len(cells); k++ {
			cells[k].match = max(cells[k].match, matchOccurrence)
		}
	}
}

// markDiagnostics records the most severe diagnostic of each cell of line i.
// Spans with a severity outside the LSP range are ignored.
func (f frame) markDiagnostics(i int, raw []rune, cells []cell) {
	if f.diags == nil {
		return
	}
	for _, d := range f.diags(i) {
		if d.Severity < SeverityError || d.Severity > SeverityHint {
			continue
		}
		from, to := expandedIndex(raw, d.Start-f.off, f.tab), expandedIndex(raw, d.End-f.off, f.tab)
		for k := from; k < to && k < len(cells); k++ {
			if cells[k].diag == 0 || d.Severity < cells[k].diag {
				cells[k].diag = d.Severity
			}
		}
	}
}

// diagnosticFG is the underline color of a severity, or nil when the theme gives
// none.
func (f frame) diagnosticFG(sev int) color.Color {
	switch sev {
	case SeverityError:
		return f.styles.DiagError.FG
	case SeverityWarning:
		return f.styles.DiagWarning.FG
	case SeverityInfo:
		return f.styles.DiagInfo.FG
	case SeverityHint:
		return f.styles.DiagHint.FG
	}
	return nil
}

// paintRulers tints the ruler columns of the text rows. A ruler shows only on the
// plain background, so a selection, a match, an occurrence or the current line
// keeps its own color.
func (f frame) paintRulers(g *grid.Grid) {
	if f.styles.Ruler.BG == nil {
		return
	}
	for _, col := range f.rulers {
		x := f.gutter + col - 1 - f.left
		if col < 1 || x < f.gutter || x >= f.w-1 {
			continue
		}
		for y := range f.h {
			c := g.At(x, y)
			if c.Text == "" || c.Style.BG != f.styles.Text.BG {
				continue
			}
			st := c.Style
			st = withBackground(st, f.styles.Ruler)
			g.Set(x, y, c.Text, st)
		}
	}
}

// wordAt returns the word that the caret at col of runes is in, or just after,
// or "" when there is none.
func wordAt(runes []rune, col int) string {
	var at int
	switch {
	case col < len(runes) && isWordRune(runes[col]):
		at = col
	case col > 0 && col <= len(runes) && isWordRune(runes[col-1]):
		at = col - 1
	default:
		return ""
	}
	lo, hi := at, at+1
	for lo > 0 && isWordRune(runes[lo-1]) {
		lo--
	}
	for hi < len(runes) && isWordRune(runes[hi]) {
		hi++
	}
	return string(runes[lo:hi])
}

// isWordRune is the word character of the editor for the occurrence rule: letters,
// digits, marks and the underscore.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) || r == '_'
}
