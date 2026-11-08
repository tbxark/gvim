package editor

// registerTextCmdHandlers registers the text commands: sort, select by level,
// duplicate above, trim, final newline, and copy as plain text.
func registerTextCmdHandlers(h map[Action]handler) {
	h["sort_lines"] = docAction(func(_ *Editor, d *Document, _ map[string]any) error {
		if d.LargeState() != nil && !d.HasAnySelection() {
			// Sorting the whole document reads every line of it.
			return errLargeAction("sort_lines")
		}
		d.SortLines()
		return nil
	})
	h["expand_selection"] = docAction(func(_ *Editor, d *Document, _ map[string]any) error {
		d.ExpandSelection()
		return nil
	})
	h["duplicate_line_above"] = docAction(func(_ *Editor, d *Document, _ map[string]any) error {
		d.DuplicateLines(true)
		return nil
	})
	h["trim_trailing_whitespace"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		if d.TrimTrailing() {
			e.setStatus("Trimmed trailing whitespace")
		} else {
			e.setStatus("No trailing whitespace to remove")
		}
		return nil
	})
	h["ensure_final_newline"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		if d.EnsureFinalLineBreak() {
			e.setStatus("Added final newline")
		} else {
			e.setStatus("File already ends with newline")
		}
		return nil
	})
	// copy_with_theme copies the plain text. The syntax colours as rich text are the
	// root's part (ui.copyFormatted), since they need the theme and the clipboard.
	h["copy_with_theme"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		if !d.HasAnySelection() {
			return ErrNothingSelected
		}
		text, _, err := d.CopyText()
		if err != nil {
			return err
		}
		if text == "" {
			return ErrNothingSelected
		}
		e.clipboard, e.resultText = text, text
		e.setStatus("Copied as plain text")
		return nil
	})
}
