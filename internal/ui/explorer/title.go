package explorer

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Header is the title row of the explorer panel. Text is exactly the panel
// width in cells. Reload and Close are the columns of the "⟳" and "×" buttons,
// and -1 where the width leaves no room for the button (blueprint section 4.3).
type Header struct {
	Text   string
	Reload int
	Close  int
}

// headText is the title without its buttons. It is 16 cells wide.
const headText = "┌ File Explorer "

// headerFixed is the width the title needs before its "─" fill: the head, the
// reload button, the close button and the corner.
const headerFixed = 21

// HeaderFor returns the title row for a panel width columns wide. The corner,
// the "×" and the "⟳" drop out in that order as the panel narrows.
func HeaderFor(width int) Header {
	text := headerText(width)
	return Header{
		Text:   text,
		Reload: column(text, "⟳"),
		Close:  column(text, "×"),
	}
}

// headerText is the title string for a panel width columns wide.
func headerText(width int) string {
	switch {
	case width <= 16:
		return ansi.Truncate(headText, max(width, 0), "")
	case width == 17:
		return headText + "⟳"
	case width == 18:
		return headText + "⟳┐"
	case width == 19:
		return headText + "⟳─×"
	case width == 20:
		return headText + "⟳─×┐"
	default:
		return headText + strings.Repeat("─", width-headerFixed) + "⟳─×─┐"
	}
}

// column returns the first column of glyph in text, or -1 when text has none.
func column(text, glyph string) int {
	before, _, ok := strings.Cut(text, glyph)
	if !ok {
		return -1
	}
	return ansi.StringWidth(before)
}
