package editor

import (
	"errors"
	"strings"
)

// ErrBinary is returned by an action that would write a binary document, and by
// toggle_read_only, which would make it editable. A binary file is shown, not
// edited: its text is not the bytes of the file.
var ErrBinary = errors.New("binary file")

// binaryDocument is the read-only document for a file that internal/encoding calls
// binary. Each byte shows as one character: the bytes of Latin-1, except that a
// control byte shows as its Unicode control picture and a C1 control as U+FFFD,
// so nothing in the text can act on the terminal. The line endings are left as
// they are.
func binaryDocument(path string, data []byte, disk diskState) *Document {
	var b strings.Builder
	for _, c := range data {
		switch {
		case c == '\t' || c == '\n' || c == '\r':
			b.WriteByte(c)
		case c < 0x20:
			b.WriteRune(0x2400 + rune(c))
		case c == 0x7f:
			b.WriteRune(0x2421)
		case c >= 0x80 && c < 0xa0:
			b.WriteRune('�')
		default:
			b.WriteRune(rune(c))
		}
	}
	d := newDocument(path, b.String())
	d.disk = disk
	d.codec = "iso-8859-1"
	d.encoding = "Binary"
	d.readOnly = true
	d.binary = true
	return d
}

// Binary reports whether the document is a binary file.
func (d *Document) Binary() bool { return d.binary }
