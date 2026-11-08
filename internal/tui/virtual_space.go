package tui

// virtualLinesEditor is the optional part of EditorState that parks the caret on
// a virtual line below the last line (vertical virtual space): a click under the
// last line asks for it. An editor that does not implement it keeps the clamped
// caret, so a press below the text places the caret at the buffer end.
type virtualLinesEditor interface {
	// SetVirtualLines parks the caret lines rows below the last line at display
	// column col, and reports whether the editor allowed it.
	SetVirtualLines(lines, col int) bool
}
