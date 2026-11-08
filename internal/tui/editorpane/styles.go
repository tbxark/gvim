package editorpane

import (
	"image/color"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/syntax"
	"github.com/tbxark/gvim/theme"
)

// numKinds is the number of syntax kinds, KindPlain included.
const numKinds = int(syntax.KindCount)

// Styles are the cell attributes the pane paints with. NewStyles builds them
// from a theme. A zero Styles paints with the terminal defaults.
type Styles struct {
	Text         grid.Style           // editor fg on editor bg; the body and "~" rows
	Selection    grid.Style           // only BG is used: the selection background
	Gutter       grid.Style           // line numbers, the margin bar and the change mark
	Scrollbar    grid.Style           // the track glyph: the track color as its foreground
	Thumb        grid.Style           // the thumb glyph: the thumb color as its foreground
	Match        grid.Style           // only BG is used: the background of a search match
	CurrentMatch grid.Style           // the current search match: fg, bg and bold
	Syntax       [numKinds]grid.Style // per syntax kind: Text with the kind's foreground
	// SecondaryCaret paints the carets other than the primary one.
	SecondaryCaret grid.Style
	// CurrentLine is the background of the caret's text row. Only BG is used.
	CurrentLine grid.Style
	// Ruler is the background of a ruler column. Only BG is used.
	Ruler grid.Style
	// Whitespace is the color of visible whitespace and indent guides.
	Whitespace grid.Style
	// Occurrence is the background of the occurrences of the word at the caret.
	Occurrence grid.Style
	// DiagError, DiagWarning, DiagInfo and DiagHint give the color of a
	// diagnostic underline by severity. Only FG is used.
	DiagError, DiagWarning, DiagInfo, DiagHint grid.Style
	// Decor are the foregrounds of guides, bracket highlights and the fold placeholder.
	Decor DecorStyles
}

// NewStyles returns the pane styles for th. The text and gutter roles come from
// style.New. Each syntax kind keeps the text background and takes its foreground
// from the theme's "syntax" section. A kind with no RGB value in that section
// takes the editor foreground, and a kind set to the terminal default takes no
// foreground, as the legacy painter does.
func NewStyles(th theme.Theme) Styles {
	chrome := style.New(th)
	text := style.Cell(chrome.Editor)
	st := Styles{
		Text:         text,
		Selection:    style.Cell(chrome.Selection),
		Gutter:       style.Cell(chrome.Gutter),
		Scrollbar:    style.Cell(chrome.Scrollbar),
		Thumb:        style.Cell(chrome.ScrollbarThumb),
		Match:        style.Cell(chrome.Match),
		CurrentMatch: style.Cell(chrome.CurrentMatch),

		SecondaryCaret: style.Cell(chrome.SecondaryCaret),
		CurrentLine:    style.Cell(chrome.CurrentLine),
		Ruler:          style.Cell(chrome.Ruler),
		Whitespace:     style.Cell(chrome.Whitespace),
		Occurrence:     style.Cell(chrome.Occurrence),
		DiagError:      style.Cell(chrome.DiagError),
		DiagWarning:    style.Cell(chrome.DiagWarning),
		DiagInfo:       style.Cell(chrome.DiagInfo),
		DiagHint:       style.Cell(chrome.DiagHint),
		Decor:          newDecorStyles(chrome.Decor, text, style.Cell(chrome.Gutter)),
	}
	for k := range st.Syntax {
		kind := syntax.Kind(k)
		st.Syntax[k] = text
		if kind == syntax.KindPlain {
			continue
		}
		st.Syntax[k].FG = syntaxFG(th, kind, text.FG)
		attrs := kind.Attrs()
		st.Syntax[k].Bold = attrs&syntax.AttrBold != 0
		st.Syntax[k].Italic = attrs&syntax.AttrItalic != 0
		st.Syntax[k].Underline = attrs&syntax.AttrUnderline != 0
		st.Syntax[k].Strike = attrs&syntax.AttrStrike != 0
	}
	return st
}

// syntaxFG resolves the foreground of one syntax kind. base is the editor
// foreground, which a missing or named value falls back to.
func syntaxFG(th theme.Theme, kind syntax.Kind, base color.Color) color.Color {
	key := kind.ThemeKey()
	c, ok := th.Syntax(key)
	if fb := kind.ThemeFallbackKey(); !ok && fb != "" {
		key = fb
		c, ok = th.Syntax(key)
	}
	switch {
	case !ok:
		return base
	case c.IsTerminalDefault():
		return nil
	case c.IsRGB:
		return style.Color(th, "syntax", key)
	default:
		return base
	}
}
