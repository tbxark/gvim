package space

// The centred card box that the dialogs (internal/tui/dialog), the prompts
// (internal/tui/prompt) and the trust warning share: a rounded border with the
// title on the top rule (style.Overlay.PaintBox), the content inset by two
// cells on the left, and one border cell on every side.

const (
	// CardChrome is the cells a card box adds to its content width: a border
	// cell and a padding cell on each side.
	CardChrome = 4

	cardMinContent = 40
	cardMaxContent = 64
)

// CardWidth returns the content width for a card whose body is natural cells
// wide: at least 40, at most 64, and no wider than the frame allows.
func CardWidth(frameW, natural int) int {
	cw := min(max(natural, cardMinContent), cardMaxContent)
	cw = min(cw, frameW-CardChrome-2*Margin(frameW))
	return max(cw, 1)
}

// CardBox returns the box rectangle for a body of cw cells across bodyH rows in
// a frame of fw by fh cells: the box is CardChrome wider than the body and two
// rows taller, centred on both axes and clamped into the frame.
func CardBox(fw, fh, cw, bodyH int) Rect {
	w := cw + CardChrome
	h := bodyH + 2 // the top border with the title, the body, the bottom border
	return Rect{
		X: max((fw-w)/2, 0),
		Y: max((fh-h)/2, 0),
		W: w,
		H: h,
	}
}
