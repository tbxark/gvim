package editor

import (
	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/document"
	"github.com/tbxark/gvim/i18n"
)

// copyAction copies the selections, or the lines of the carets.
func copyAction(e *Editor, d *document.Document, _ map[string]any) error {
	text, lines, err := d.CopyText()
	if err != nil {
		return err
	}
	if text == "" {
		return document.ErrNothingSelected
	}
	e.clipboard, e.resultText = text, text
	if lines {
		e.setStatus(i18n.T("", "clipboard.copied_line", nil))
	} else {
		e.setStatus(i18n.T("", "clipboard.copied", nil))
	}
	return nil
}

// cutAction copies like copyAction and deletes what it copied.
func cutAction(e *Editor, d *document.Document, _ map[string]any) error {
	text, lines, err := d.CopyText()
	if err != nil {
		return err
	}
	if text == "" {
		return document.ErrNothingSelected
	}
	if lines {
		d.CutLines()
		if d.EditRefused() {
			return nil
		}
		e.clipboard, e.resultText = text, text
		e.setStatus(i18n.T("", "clipboard.cut_line", nil))
		return nil
	}
	// The deletion never materializes a virtual block's padding: the gap holds
	// no text to delete.
	d.DeleteAtCarets()
	if d.EditRefused() {
		return nil
	}
	e.clipboard, e.resultText = text, text
	e.setStatus(i18n.T("", "clipboard.cut", nil))
	return nil
}

// pasteAction inserts the "text" argument, which the UI fills from the system
// clipboard or a bracketed paste, or else the private register.
func pasteAction(e *Editor, d *document.Document, args map[string]any) error {
	text, ok := args["text"].(string)
	if !ok {
		text = e.clipboard
	}
	if len(text) > buffer.MaxMaterialize {
		return document.ErrPasteTooLarge
	}
	if text = document.NormalizePaste(text); text != "" {
		d.PasteText(text)
		e.setStatus(i18n.T("", "clipboard.pasted", nil))
	}
	return nil
}
