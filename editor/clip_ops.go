package editor

import (
	"github.com/tbxark/gvim/buffer"
)

// copyAction copies the selections, or the lines of the carets.
func copyAction(e *Editor, d *Document, _ map[string]any) error {
	text, lines, err := d.CopyText()
	if err != nil {
		return err
	}
	if text == "" {
		return ErrNothingSelected
	}
	e.clipboard, e.resultText = text, text
	if lines {
		e.setStatus("Copied line")
	} else {
		e.setStatus("Copied")
	}
	return nil
}

// cutAction copies like copyAction and deletes what it copied.
func cutAction(e *Editor, d *Document, _ map[string]any) error {
	text, lines, err := d.CopyText()
	if err != nil {
		return err
	}
	if text == "" {
		return ErrNothingSelected
	}
	e.clipboard, e.resultText = text, text
	if lines {
		d.CutLines()
		e.setStatus("Cut line")
		return nil
	}
	// The deletion never materializes a virtual block's padding: the gap holds
	// no text to delete.
	d.DeleteAtCarets()
	e.setStatus("Cut")
	return nil
}

// pasteAction inserts the "text" argument, which the UI fills from the system
// clipboard or a bracketed paste, or else the private register.
func pasteAction(e *Editor, d *Document, args map[string]any) error {
	text, ok := args["text"].(string)
	if !ok {
		text = e.clipboard
	}
	if len(text) > buffer.MaxMaterialize {
		return ErrPasteTooLarge
	}
	if text = normalizePaste(text); text != "" {
		d.PasteText(text)
		e.setStatus("Pasted")
	}
	return nil
}
