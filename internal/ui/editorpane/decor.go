package editorpane

import (
	"image/color"
	"strings"
	"unicode"

	"github.com/tbxark/gvim/internal/display"
	"github.com/tbxark/gvim/internal/ui/grid"
)

// guideGlyph is the indent guide drawn in the leading blanks.
const guideGlyph = '│'

// markOccurrences sets the occurrence mark of each whole-word match of the word at
// the caret. It is set on the cells of the line, which the search matches then
// override.
func (f frame) markOccurrences(raw []rune, cells []cell) {
	if f.occWord == "" || f.styles.Occurrence.BG == nil {
		return
	}
	for _, s := range display.Occurrences([]string{string(raw)}, f.occWord) {
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
		from, to := expandedIndex(raw, d.Start, f.tab), expandedIndex(raw, d.End, f.tab)
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

// markWhitespace replaces the blanks of a line with the visible whitespace marks
// and draws the indent guides, when those options are on. The marks come from
// display.VisibleWhitespace, which gives one rune per cell of the expanded line,
// so the two sequences line up. A carriage return has no cell and is left out.
func (f frame) markWhitespace(text string, cells []cell) {
	if f.visWS {
		vis := []rune(display.VisibleWhitespace(strings.ReplaceAll(text, "\r", ""), f.tab))
		if len(vis) == len(cells) {
			for j, r := range vis {
				if r == '→' && f.noTabArrows {
					r = ' '
				}
				if r != cells[j].r {
					cells[j].r, cells[j].ws = r, true
				}
			}
		}
	}
	if f.guides {
		// A guide takes the place of a blank or a space mark, and leaves a tab arrow.
		for _, col := range display.IndentGuides(text, f.tab) {
			if col < len(cells) && (cells[col].r == ' ' || cells[col].r == selectedBlank) {
				cells[col].r, cells[col].ws = guideGlyph, true
			}
		}
	}
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
			st.BG = f.styles.Ruler.BG
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
