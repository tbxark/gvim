package document

import (
	"errors"
	"github.com/tbxark/gvim/buffer"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrVirtual is returned by Save and Save As of a virtual document, which has no file.
var ErrVirtual = errors.New("a virtual buffer is not a file and cannot be saved")

// MaxVirtualText is the longest text of a virtual document, in bytes.
const MaxVirtualText = buffer.MaxMaterialize

// maxVirtualName bounds the name of a virtual document, in runes.
const maxVirtualName = 64

// VirtualOptions describes a new virtual document. Text uses "\n" as its line break.
type VirtualOptions struct {
	Name, Language, Mode, Text string
	// Owner names who made the document (the plugin); the quotas of a plugin count the
	// documents and the text of its owner.
	Owner    string
	ReadOnly bool
}

// PropSpan is the properties of one run of a virtual document's text: [From, To) in
// lines and UTF-8 byte columns of the document, as a plugin sees them. Spans of one
// document are sorted and do not overlap.
type PropSpan struct {
	FromLine, FromCol, ToLine, ToCol int
	Props                            map[string]any
}

// docExt is the plugin-facing state of a document: whether it is virtual, its name, the
// plugin's buffer-local input mode (any document may have one) and the text properties.
type docExt struct {
	Virtual bool
	Name    string // the name inside the asterisks of the tab
	Owner   string // who made the virtual document
	Size    int    // bytes of the text last set by the owner (not the user's typing)
	Mode    string
	Props   []PropSpan
	// propsAt is the content state the props describe. They answer nothing for any other
	// state, so a document the user edited since the plugin set it has no stale props.
	PropsAt Version
}

// Virtual reports whether the document is a virtual one.
func (d *Document) Virtual() bool { return d.ext.Virtual }

// Mode returns the name of the buffer-local input mode a plugin set, or "".
func (d *Document) Mode() string { return d.ext.Mode }

// cleanVirtualName makes name fit a tab: control characters become blanks, the ends are
// trimmed and the length is bounded.
func cleanVirtualName(name string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return ' '
		}
		return r
	}, name))
	if r := []rune(name); len(r) > maxVirtualName {
		name = string(r[:maxVirtualName])
	}
	if name == "" {
		name = "virtual"
	}
	return name
}

// cmpLineCol orders two (line, byte column) positions.
func cmpLineCol(l1, c1, l2, c2 int) int {
	if l1 != l2 {
		return l1 - l2
	}
	return c1 - c2
}
