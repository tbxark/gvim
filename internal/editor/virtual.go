package editor

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/buffer"
)

// Virtual documents (FINAL X10, stage S18): the scratch tabs a plugin owns. A virtual
// document is not a file. Its Path is "", its tab is named *name*, and it is never
// modified: closing it asks nothing, and it is left out of the saved session, the
// crash-recovery copy, the language-server sync, auto-save, Save, the disk check and the
// dirty list of a quit. Its text is replaced whole by SetVirtualText, which swaps the
// Buffer for a new one (the same funnel as a revert), so its undo history cannot grow
// with every refresh. Text properties of the content (the plugin's per-range tables) live
// in a side table that is valid for one content state only.

// ErrNotVirtual is returned by an operation that exists for virtual documents only.
var ErrNotVirtual = errors.New("not a virtual buffer")

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
	virtual bool
	name    string // the name inside the asterisks of the tab
	owner   string // who made the virtual document
	size    int    // bytes of the text last set by the owner (not the user's typing)
	mode    string
	props   []PropSpan
	// propsAt is the content state the props describe. They answer nothing for any other
	// state, so a document the user edited since the plugin set it has no stale props.
	propsAt Version
}

// Virtual reports whether the document is a virtual one.
func (d *Document) Virtual() bool { return d.ext.virtual }

// Mode returns the name of the buffer-local input mode a plugin set, or "".
func (d *Document) Mode() string { return d.ext.mode }

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

// NewVirtual opens a virtual document as the last tab and returns its id. It does not
// make the tab active.
func (e *Editor) NewVirtual(o VirtualOptions) DocID {
	text, _ := splitLineEnding(o.Text)
	d := newDocument("", text)
	d.eol = eolLF
	d.language = o.Language
	d.readOnly = o.ReadOnly
	d.ext = docExt{virtual: true, name: cleanVirtualName(o.Name), owner: o.Owner, size: len(text), mode: o.Mode}
	e.addDoc(d)
	return d.seam.id
}

// VirtualCount returns the number of open virtual documents.
func (e *Editor) VirtualCount() int {
	n := 0
	for _, d := range e.docs {
		if d.ext.virtual {
			n++
		}
	}
	return n
}

// VirtualUsage returns the number of open virtual documents that owner made and the bytes
// of text it set in them.
func (e *Editor) VirtualUsage(owner string) (count, bytes int) {
	return e.virtualUsage(func(o string) bool { return o == owner })
}

// VirtualTotals returns the number of open virtual documents and the bytes of text set in
// them, whoever made them.
func (e *Editor) VirtualTotals() (count, bytes int) {
	return e.virtualUsage(func(string) bool { return true })
}

func (e *Editor) virtualUsage(match func(owner string) bool) (count, bytes int) {
	for _, d := range e.docs {
		if d.ext.virtual && match(d.ext.owner) {
			count++
			bytes += d.ext.size
		}
	}
	return count, bytes
}

// VirtualOwner returns the owner and the size in bytes of the text last set in the
// virtual document id.
func (e *Editor) VirtualOwner(id DocID) (owner string, size int, err error) {
	d, err := e.virtualDoc(id)
	if err != nil {
		return "", 0, err
	}
	return d.ext.owner, d.ext.size, nil
}

// virtualDoc returns the virtual document with the given id.
func (e *Editor) virtualDoc(id DocID) (*Document, error) {
	d, _, err := e.docByID(id)
	if err != nil {
		return nil, err
	}
	if !d.ext.virtual {
		return nil, fmt.Errorf("%w: id %d", ErrNotVirtual, id)
	}
	return d, nil
}

// SetVirtualText replaces the whole text of a virtual document without marking it
// modified. The caret keeps its line (and its column, as far as the new line has one),
// the scroll position stays when the text still reaches it, and the selection, the other
// carets, the folds, the undo history and the text properties are dropped: they refer to
// the old text. The Buffer is replaced, so the document's Version gets a new generation, and the new Buffer's version
// numbers start after every one the old Buffer issued, so a cache keyed on the Buffer version
// alone cannot take the new text for the old.
func (e *Editor) SetVirtualText(id DocID, text string) error {
	d, err := e.virtualDoc(id)
	if err != nil {
		return err
	}
	if len(text) > MaxVirtualText {
		return fmt.Errorf("%w: virtual text is over %d bytes", buffer.ErrOverCap, MaxVirtualText)
	}
	text, _ = splitLineEnding(text)
	cursor, top := d.Cursor, d.Top
	old := d.Buffer
	d.setBuffer(buffer.NewAfter(text, old))
	_ = old.Close()
	d.ext.size = len(text)
	d.setSaved(text)
	d.Sel = nil
	d.clearCarets()
	d.folded = nil
	d.want = -1
	d.resetHistoryState()
	d.Cursor = d.Buffer.Clamp(cursor)
	d.Top = max(min(top, d.Buffer.Lines()-1), 0)
	d.ext.props = nil
	return nil
}

// SetVirtualProps records the properties of the current text of a virtual document, in
// place of the ones before. They answer VirtualProps until the text changes.
func (e *Editor) SetVirtualProps(id DocID, spans []PropSpan) error {
	d, err := e.virtualDoc(id)
	if err != nil {
		return err
	}
	d.ext.props = slices.Clone(spans)
	d.ext.propsAt = d.Version()
	return nil
}

// VirtualProps returns the properties of the run of text that covers the position (line,
// UTF-8 byte column), or nil when none does. The end of the document belongs to the last
// run when it ends there. A document edited since SetVirtualProps has none.
func (e *Editor) VirtualProps(id DocID, line, col int) (map[string]any, error) {
	d, err := e.virtualDoc(id)
	if err != nil {
		return nil, err
	}
	if len(d.ext.props) == 0 || d.ext.propsAt != d.Version() {
		return nil, nil
	}
	b := d.Buffer
	if last := b.Lines() - 1; line > last {
		line, col = last, b.LineBytes(last)
	}
	// n is the first span that starts after the position; the one before it is the only
	// candidate, as spans do not overlap.
	n := sort.Search(len(d.ext.props), func(i int) bool {
		return cmpLineCol(d.ext.props[i].FromLine, d.ext.props[i].FromCol, line, col) > 0
	})
	if n == 0 {
		return nil, nil
	}
	s := d.ext.props[n-1]
	switch c := cmpLineCol(line, col, s.ToLine, s.ToCol); {
	case c < 0:
		return s.Props, nil
	case c == 0 && line == b.Lines()-1 && col == b.LineBytes(line):
		return s.Props, nil // the end of the document belongs to the run that ends there
	}
	return nil, nil
}

// cmpLineCol orders two (line, byte column) positions.
func cmpLineCol(l1, c1, l2, c2 int) int {
	if l1 != l2 {
		return l1 - l2
	}
	return c1 - c2
}

// SetReadOnlyID sets the read-only flag of a virtual document.
func (e *Editor) SetReadOnlyID(id DocID, ro bool) error {
	d, err := e.virtualDoc(id)
	if err != nil {
		return err
	}
	d.readOnly = ro
	return nil
}

// SetModeID sets the buffer-local input mode of any document; "" clears it.
func (e *Editor) SetModeID(id DocID, mode string) error {
	d, _, err := e.docByID(id)
	if err != nil {
		return err
	}
	d.ext.mode = mode
	return nil
}

// UpdateViewSettings lets fn change the per-document display settings of document id,
// as the per-buffer view actions do.
func (e *Editor) UpdateViewSettings(id DocID, fn func(*ViewSettings)) error {
	d, _, err := e.docByID(id)
	if err != nil {
		return err
	}
	v := d.view
	fn(&v)
	d.view = v
	return nil
}
