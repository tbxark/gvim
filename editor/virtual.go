package editor

import (
	"errors"
	"fmt"
	"github.com/tbxark/gvim/buffer"
	"slices"
	"sort"
)

// ErrNotVirtual is returned by an operation that exists for virtual documents only.
var ErrNotVirtual = errors.New("not a virtual buffer")

// NewVirtual opens a virtual document as the last tab and returns its id. It does not
// make the tab active.
func (e *Editor) NewVirtual(o VirtualOptions) DocID {
	text, _ := splitLineEnding(o.Text)
	d := newDocument("", text)
	d.Eol = eolLF
	d.SetLanguage(o.Language)
	d.SetReadOnly(o.ReadOnly)
	d.SetExt(docExt{Virtual: true, Name: cleanVirtualName(o.Name), Owner: o.Owner, Size: len(text), Mode: o.Mode})
	e.addDoc(d)
	return d.Seam().ID
}

// VirtualCount returns the number of open virtual documents.
func (e *Editor) VirtualCount() int {
	n := 0
	for _, d := range e.docs {
		if d.Ext().Virtual {
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
		if d.Ext().Virtual && match(d.Ext().Owner) {
			count++
			bytes += d.Ext().Size
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
	return d.Ext().Owner, d.Ext().Size, nil
}

// virtualDoc returns the virtual document with the given id.
func (e *Editor) virtualDoc(id DocID) (*Document, error) {
	d, _, err := e.docByID(id)
	if err != nil {
		return nil, err
	}
	if !d.Ext().Virtual {
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
	d.SetBuffer(buffer.NewAfter(text, old))
	_ = old.Close()
	d.Ext().Size = len(text)
	d.SetSaved(text)
	d.Sel = nil
	d.ClearCarets()
	d.SetFoldedLines(nil)
	d.Want = -1
	d.ResetHistoryState()
	d.Cursor = d.Buffer.Clamp(cursor)
	d.Top = max(min(top, d.Buffer.Lines()-1), 0)
	d.Ext().Props = nil
	return nil
}

// SetVirtualProps records the properties of the current text of a virtual document, in
// place of the ones before. They answer VirtualProps until the text changes.
func (e *Editor) SetVirtualProps(id DocID, spans []PropSpan) error {
	d, err := e.virtualDoc(id)
	if err != nil {
		return err
	}
	d.Ext().Props = slices.Clone(spans)
	d.Ext().PropsAt = d.Version()
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
	if len(d.Ext().Props) == 0 || d.Ext().PropsAt != d.Version() {
		return nil, nil
	}
	b := d.Buffer
	if last := b.Lines() - 1; line > last {
		line, col = last, b.LineBytes(last)
	}
	// n is the first span that starts after the position; the one before it is the only
	// candidate, as spans do not overlap.
	n := sort.Search(len(d.Ext().Props), func(i int) bool {
		return cmpLineCol(d.Ext().Props[i].FromLine, d.Ext().Props[i].FromCol, line, col) > 0
	})
	if n == 0 {
		return nil, nil
	}
	s := d.Ext().Props[n-1]
	switch c := cmpLineCol(line, col, s.ToLine, s.ToCol); {
	case c < 0:
		return s.Props, nil
	case c == 0 && line == b.Lines()-1 && col == b.LineBytes(line):
		return s.Props, nil // the end of the document belongs to the run that ends there
	}
	return nil, nil
}

// SetReadOnlyID sets the read-only flag of a virtual document.
func (e *Editor) SetReadOnlyID(id DocID, ro bool) error {
	d, err := e.virtualDoc(id)
	if err != nil {
		return err
	}
	d.SetReadOnly(ro)
	return nil
}

// SetModeID sets the buffer-local input mode of any document; "" clears it.
func (e *Editor) SetModeID(id DocID, mode string) error {
	d, _, err := e.docByID(id)
	if err != nil {
		return err
	}
	d.Ext().Mode = mode
	return nil
}

// UpdateViewSettings lets fn change the per-document display settings of document id,
// as the per-buffer view actions do.
func (e *Editor) UpdateViewSettings(id DocID, fn func(*ViewSettings)) error {
	d, _, err := e.docByID(id)
	if err != nil {
		return err
	}
	fn(d.View())
	return nil
}
