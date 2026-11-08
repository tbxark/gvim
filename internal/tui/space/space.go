// Package space holds the spacing and text-fitting system of the terminal UI:
// the paddings and margins that components share, so a bar, a menu, a popup and
// a dialog breathe the same way and narrow terminals give the room back in one
// place, and the helpers that fit text to a cell budget (End, Tail, Middle,
// Path, Name, Wrap, Window and PadTo). Rect is the shared rectangle type of the
// layout and of the overlay kits.
//
// The rules, in cells:
//
//   - PadX: horizontal padding between a border (or the screen edge of a bar) and
//     the text inside it. It applies to bars, menus, popups, dialogs, list rows and
//     the explorer.
//   - Margin(w): the gap between a floating box (palette, popup, list, browser) and
//     the screen edge. It is 0 below 60 columns, 1 up to 139 and 2 beyond.
//   - ListMargin(w): the same gap for the boxes that are centred: two cells from 80
//     columns, one from 60, none below.
//   - Bar items (status items, filter options) carry PadX on both sides, so two
//     neighbours are two cells apart.
//   - Rows of a dialog: one blank row between the border and the content.
//
// A frame narrower than NarrowWidth drops the optional space, never the caret
// column: Margin is then at most 1, and PadX stays where text would otherwise
// touch a border.
package space

import "strings"

const (
	// PadX is the horizontal padding inside a border or bar.
	PadX = 1
	// NarrowWidth is the frame width below which the optional space shrinks.
	NarrowWidth = 80
)

// Margin is the gap between a floating box and the left and right screen edge in a
// frame w cells wide.
func Margin(w int) int {
	switch {
	case w < 60:
		return 0
	case w < 140:
		return 1
	}
	return 2
}

// ListMargin is the gap between a centred box (a list, a text box, the file browser,
// a dialog) and the left and right screen edge in a frame w cells wide: two cells
// from NarrowWidth up, one down to 60 columns, none below.
func ListMargin(w int) int {
	switch {
	case w >= NarrowWidth:
		return 2
	case w >= 60:
		return 1
	}
	return 0
}

// Pad surrounds s with PadX blank cells on each side: the label of a bar item, a tab
// or a status segment.
func Pad(s string) string {
	pad := strings.Repeat(" ", PadX)
	return pad + s + pad
}
