package sidebar

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/internal/tui/style"
)

// HeaderSpec describes one section's header row for HeaderFor.
type HeaderSpec struct {
	Kind Kind
	// First makes the row the column's top border (┌ ┐); otherwise it is the
	// row shared with the section above (├ ┤).
	First bool
	// Several says the column has more than one section, so the explorer's
	// header shows its collapse chevron. A lone explorer keeps the cells it has
	// always had.
	Several bool
	// Collapsed points the chevron right (▶) instead of down (▼).
	Collapsed bool
}

// Header is one section's header row: its text, exactly width cells, and the
// columns of its buttons. -1 means the width left no room for the button.
type Header struct {
	Text   string
	Reload int // the explorer's refresh button
	Close  int // the × close button
}

// HeaderFor returns the header row of a section of the given width. The title
// sits after the left corner, the explorer's chevron when the column has more
// than one section, and one padding cell. The explorer's ⟳ and × buttons are
// pinned to the right edge; a section that is not the explorer has only its ×.
// As the row narrows the corner, then the buttons, drop out in the explorer's
// established order, so the title never pushes a control off the row.
func HeaderFor(width int, m icons.Mode, spec HeaderSpec) Header {
	corner := '┌'
	if !spec.First {
		corner = '├'
	}
	right := '┐'
	if !spec.First {
		right = '┤'
	}
	var head strings.Builder
	head.WriteRune(corner)
	head.WriteString(" ")
	if spec.Several {
		chevron := icons.DirExpanded
		if spec.Collapsed {
			chevron = icons.DirCollapsed
		}
		head.WriteString(m.Glyph(chevron))
		head.WriteString(" ")
	}
	switch spec.Kind {
	case KindContents:
		head.WriteString("Contents ")
	default:
		head.WriteString("File Explorer ")
	}
	headText := head.String()
	headWidth := ansi.StringWidth(headText)
	reload, closeGlyph := m.Glyph(icons.Refresh), m.Glyph(icons.Close)
	h := Header{Reload: -1, Close: -1}

	if spec.Kind == KindExplorer {
		// The fixed width is the head, ⟳, "─", ×, "─" and the corner.
		fixed := headWidth + 5
		switch {
		case width <= headWidth:
			h.Text = ansi.Truncate(headText, max(width, 0), "")
			return h
		case width == headWidth+1:
			h.Text = headText + reload
		case width == headWidth+2:
			h.Text = headText + reload + string(right)
		case width == headWidth+3:
			h.Text = headText + reload + "─" + closeGlyph
		case width == headWidth+4:
			h.Text = headText + reload + "─" + closeGlyph + string(right)
		default:
			h.Text = headText + strings.Repeat("─", width-fixed) + reload + "─" + closeGlyph + "─" + string(right)
		}
		h.Reload = headWidth + max(width-fixed, 0)
		if width >= headWidth+3 {
			h.Close = h.Reload + 2
		}
		return h
	}

	// A section other than the explorer: the × then the corner. The fixed width
	// is the head, × and the corner with one rule cell before it.
	fixed := headWidth + 3
	switch {
	case width <= headWidth:
		h.Text = ansi.Truncate(headText, max(width, 0), "")
		return h
	case width == headWidth+1:
		h.Text = headText + closeGlyph
	case width == headWidth+2:
		h.Text = headText + closeGlyph + string(right)
	default:
		h.Text = headText + strings.Repeat("─", width-fixed) + closeGlyph + "─" + string(right)
	}
	h.Close = headWidth + max(width-fixed, 0)
	return h
}

// PaintHeader paints h into row y of the column, on the section-header style.
func PaintHeader(dst *grid.Grid, width int, y int, h Header, st style.Styles) {
	head := style.Cell(st.ExplorerTitle)
	dst.Fill(0, width, y, head)
	dst.Put(0, y, width, h.Text, head)
}

// PaintBottomBorder paints the column's bottom border, └──┘, into row y.
func PaintBottomBorder(dst *grid.Grid, width int, y int, st style.Styles) {
	body := style.Cell(st.Explorer)
	if width < 2 {
		if width == 1 {
			dst.Set(0, y, "└", body)
		}
		return
	}
	dst.Set(0, y, "└", body)
	dst.Fill(1, width-1, y, body)
	for x := 1; x < width-1; x++ {
		dst.Set(x, y, "─", body)
	}
	dst.Set(width-1, y, "┘", body)
}

// PaintGround fills the rows of an empty column (every section collapsed)
// between first and last, inclusive, with the column's background.
func PaintGround(dst *grid.Grid, width, first, last int, st style.Styles) {
	body := style.Cell(st.Explorer)
	for y := first; y <= last; y++ {
		dst.Fill(0, width, y, body)
	}
}
