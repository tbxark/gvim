package editor

// resetBufferSettings clears the per-buffer view settings (ViewSettings, see
// viewsettings.go) to their defaults. The text, the undo history and the modified
// state of the document do not change.
func resetBufferSettings(e *Editor, d *Document, _ map[string]any) error {
	d.view = ViewSettings{}
	e.setStatus("Buffer settings reset")
	return nil
}
