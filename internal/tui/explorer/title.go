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
}

// headText is the title without its buttons. It is 16 cells wide.
const headText = "┌ File Explorer "

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
	h := Header{Reload: -1, Close: -1}
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
