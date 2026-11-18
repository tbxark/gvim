package editor

// registerDocCmdHandlers registers the document commands: the file path copies,
// revert, format, the find range, the line count, and the buffer settings reset.
// Each one is implemented in its doc_*.go file.
func registerDocCmdHandlers(h map[Action]handler) {
	h["copy_file_path"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		return copyPath(e, d, false)
	})
	h["copy_relative_file_path"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		return copyPath(e, d, true)
	})
	h["revert"] = docAction(revertDocument)
	h["format_buffer"] = formatBuffer
	h["clear_search"] = docAction(clearSearch)
	h["find_in_selection"] = docAction(findInSelection)
	h["scan_line_index"] = docAction(scanLineIndex)
	h["reset_buffer_settings"] = docAction(resetBufferSettings)
}
