package explorer

import (
	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/internal/tui/sidebar"
)

// Header is the title row of the explorer panel. Text is exactly the panel
// width in cells. Reload and Close are the columns of the refresh and close buttons,
// and -1 where the width leaves no room for the button (blueprint section 4.3).
// TitleX and TitleW locate the title in Text, zero when the row is too narrow;
// Panel.Draw repaints those cells bold.
type Header = sidebar.Header

// HeaderFor returns the title row for a panel width columns wide, with the buttons
// drawn in icon mode m. The corner, the close button and the reload button drop out
// in that order as the panel narrows. It is the sidebar's header of a lone explorer
// section, which owns the layout.
func HeaderFor(width int, m icons.Mode) Header {
	return sidebar.HeaderFor(width, m, sidebar.HeaderSpec{Kind: sidebar.KindExplorer, First: true})
}
