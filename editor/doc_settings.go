package editor

import (
	"errors"
	"fmt"
)

// resetBufferSettings clears the per-buffer view settings (ViewSettings, see
// viewsettings.go) to their defaults. The text, the undo history and the modified
// state of the document do not change.
func resetBufferSettings(e *Editor, d *Document, _ map[string]any) error {
	d.SetView(ViewSettings{})
	e.seedRulers(d)
	e.setStatus("Buffer settings reset")
	return nil
}

// setLineEnding changes the line ending that the next save writes. The text is
// not changed, but the file will be, so a new ending marks the document modified.
// Its argument "value" is "lf" or "crlf". Any other value is an error, and the
// ending stays as it was.
func setLineEnding(e *Editor, d *Document, args map[string]any) error {
	value, ok := args["value"].(string)
	if !ok {
		return errors.New("set_line_ending: missing string argument \"value\"")
	}
	var eol string
	switch value {
	case "lf":
		eol = eolLF
	case "crlf":
		eol = eolCRLF
	default:
		return fmt.Errorf("set_line_ending: value %q is not \"lf\" or \"crlf\"", value)
	}
	d.MarkMetaChanged(eol != d.Eol || d.Ends != nil)
	d.Eol = eol
	// An explicit choice writes one ending for every line.
	d.Ends = nil
	e.setStatus("Line ending " + d.Eol)
	return nil
}
