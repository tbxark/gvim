package editorpane

// PageView is the centered page view of a pane: the text lies in a page of
// Measure columns, centered between margins, with the line-number gutter, the
// fold markers and the horizontal scroll hidden. For a Markdown document the
// inline marks are composed (see compose.go): they are concealed except on the
// caret's line. The zero value is the full-width pane.
type PageView struct {
	On       bool
	Measure  int  // the page measure in cells; 0 or too wide means the text area
	Markdown bool // compose the inline Markdown marks
}

// minPageViewWidth is the narrowest page the pane lays out; a narrower measure
// would leave no room for the wrap rules to work.
const minPageViewWidth = 20

// textRight is the first column right of the text: the page's right edge in the
// page view, the scrollbar column otherwise.
func (f frame) textRight() int {
	if f.pageOn {
		return min(f.gutter+f.pageMeasure, f.w-1)
	}
	return f.w - 1
}
