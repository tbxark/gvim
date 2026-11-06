package editor

// scrollMargin is the number of lines kept between the caret and the top or
// bottom of the pane (Options.ScrollOffset), at most half the pane.
func (e *Editor) scrollMargin() int {
	return min(max(e.opts.ScrollOffset, 0), (e.rows-1)/2)
}

// ensureVisibleForJump scrolls to the caret after a jump. A jump that has to
// scroll puts the caret in the middle of the pane, so the context above and
// below it shows, as Fresh does.
func (e *Editor) ensureVisibleForJump(d *Document) {
	top := d.Top
	e.ensureVisible(d)
	if d.Top != top {
		e.recenter(d)
	}
}

// ScrollMargin is the number of rows kept between the caret and the top or bottom of
// the pane (Options.ScrollOffset), at most half the pane. The ui applies it to the
// rows of wrapped lines, which the editor counts as one line each.
func (e *Editor) ScrollMargin() int { return e.scrollMargin() }
