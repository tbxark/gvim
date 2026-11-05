package editorpane

import (
	"image/color"

	"github.com/tbxark/gvim/internal/syntax"
	"github.com/tbxark/gvim/internal/theme"
	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/style"
)

// numKinds is the number of syntax kinds, KindPlain included.
const numKinds = int(syntax.KindNumber) + 1

// Styles are the cell attributes the pane paints with. NewStyles builds them
// from a theme. A zero Styles paints with the terminal defaults.
type Styles struct {
	Text      grid.Style           // editor fg on editor bg; the body and "~" rows
	Selection grid.Style           // only BG is used: the selection background
	Gutter    grid.Style           // line numbers, the margin bar and the change mark
	Scrollbar grid.Style           // the track glyph: the track color as its foreground
	Thumb     grid.Style           // the thumb glyph: the thumb color as its foreground
	Syntax    [numKinds]grid.Style // per syntax kind: Text with the kind's foreground
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
		Text:      text,
		Selection: style.Cell(chrome.Selection),
		Gutter:    style.Cell(chrome.Gutter),
		Scrollbar: style.Cell(chrome.Scrollbar),
		Thumb:     style.Cell(chrome.ScrollbarThumb),
	}
	for k := range st.Syntax {
		kind := syntax.Kind(k)
		st.Syntax[k] = text
		if kind == syntax.KindPlain {
			continue
		}
		st.Syntax[k].FG = syntaxFG(th, kind, text.FG)
	}
	return st
}

// syntaxFG resolves the foreground of one syntax kind. base is the editor
// foreground, which a missing or named value falls back to.
func syntaxFG(th theme.Theme, kind syntax.Kind, base color.Color) color.Color {
	key := kind.ThemeKey()
	c, ok := th.Syntax(key)
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
