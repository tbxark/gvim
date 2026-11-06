package editor

import "fmt"

// scanLineIndex reports the line index of the document. The buffer keeps every line
// in memory, so the index is built as the file is read, and there is nothing to scan.
// The status gives the real line count of the text.
func scanLineIndex(e *Editor, d *Document, _ map[string]any) error {
	lines := d.Buffer.Lines()
	noun := "lines"
	if lines == 1 {
		noun = "line"
	}
	e.setStatus(fmt.Sprintf("Line index built: %d %s", lines, noun))
	return nil
}
