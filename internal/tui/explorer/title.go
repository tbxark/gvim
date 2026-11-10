package explorer

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/icons"
)

// Header is the title row of the explorer panel. Text is exactly the panel
// width in cells. Reload and Close are the columns of the refresh and close buttons,
// and -1 where the width leaves no room for the button (blueprint section 4.3).
type Header struct {
	Text   string
	Reload int
	Close  int
	// TitleX is the first cell of the title in Text and TitleW its width in cells,
	// zero when the row is too narrow. Panel.Draw repaints those cells bold.
	TitleX int
	TitleW int
}

// titleText is the panel's title without its corners or buttons.
const titleText = "File Explorer"

// headText is the title without its buttons. It is 16 cells wide.
const headText = "┌ " + titleText + " "

// headWidth is the width of headText in cells, and so the column of the first
// button when there is no "─" fill.
const headWidth = 16

// headerFixed is the width the title needs before its "─" fill: the head, the
// reload button, the close button and the corner.
const headerFixed = 21

// HeaderFor returns the title row for a panel width columns wide, with the buttons
// drawn in icon mode m. The corner, the close button and the reload button drop out
// in that order as the panel narrows.
//
// The button columns follow from the layout of the row, not from a search for the
// glyphs, so they stay right whatever the glyphs are. Every glyph is one cell wide
// in both modes (see package icons), so the row is exactly width cells.
func HeaderFor(width int, m icons.Mode) Header {
	reload, closeBtn := m.Glyph(icons.Refresh), m.Glyph(icons.Close)
	h := Header{Reload: -1, Close: -1, TitleX: 2, TitleW: ansi.StringWidth(titleText)}
	if visible := max(width-2, 0); h.TitleW > visible {
		h.TitleW = visible
	}
	switch {
	case width <= headWidth:
		h.Text = ansi.Truncate(headText, max(width, 0), "")
		return h
	case width == 17:
		h.Text = headText + reload
	case width == 18:
		h.Text = headText + reload + "┐"
	case width == 19:
		h.Text = headText + reload + "─" + closeBtn
	case width == 20:
		h.Text = headText + reload + "─" + closeBtn + "┐"
	default:
		h.Text = headText + strings.Repeat("─", width-headerFixed) + reload + "─" + closeBtn + "─┐"
	}
	h.Reload = headWidth + max(width-headerFixed, 0)
	if width >= 19 {
		h.Close = h.Reload + 2
	}
	return h
}
