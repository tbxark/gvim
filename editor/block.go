package editor

// blockHandler returns the handler of a block selection step in direction
// (dLine, dCol).
func blockHandler(dLine, dCol int) handler {
	return docAction(func(_ *Editor, d *Document, _ map[string]any) error {
		d.BlockStep(dLine, dCol)
		return nil
	})
}
