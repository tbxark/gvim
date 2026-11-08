package pluginhost

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/tbxark/gvim/decorations"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/pluginapi"
)

// Virtual buffers of the plugin host (stage S18, FINAL X10): the BufferEditor methods that
// make, fill and describe the scratch tabs plugins own. The text and the properties of the
// content live in the editor (editor/virtual.go); the styles of the content are decorations
// of the plugin decor store under the owner "virtual:<id>", so the painter that draws every
// decoration draws them, and so a closed buffer's styles are dropped with its document.

const (
	// pluginMaxModeName bounds the name of a buffer-local mode.
	pluginMaxModeName = 128
	// virtualNS is the decoration namespace of a virtual buffer's content styles.
	virtualNS = "content"
	// virtualSegmentPriority is above the priority of the style of the entry it is in, so
	// a styled segment is drawn over the entry's own style.
	virtualSegmentPriority = 1
)

// The quotas of virtual buffers (variables so that tests can use small ones).
var (
	// pluginMaxVirtual bounds the virtual buffers one plugin has open, pluginMaxVirtualAll
	// the ones open in the editor.
	pluginMaxVirtual    = 64
	pluginMaxVirtualAll = 256
	// pluginMaxVirtualBytes bounds the text one plugin keeps in its virtual buffers,
	// pluginMaxVirtualBytesAll the text of all of them. The text lives in host memory, out of
	// reach of the Lua heap guard, and a buffer costs several times its text in heap.
	pluginMaxVirtualBytes    = 64 << 20
	pluginMaxVirtualBytesAll = 256 << 20
)

// virtualOwner is the decoration owner of the content styles of buffer id.
func virtualOwner(id pluginapi.BufferID) string {
	return "virtual:" + strconv.FormatUint(uint64(id), 10)
}

// cleanText is the text of a content piece as the buffer holds it: LF line breaks and valid
// UTF-8, so the byte columns that styles and properties carry are the buffer's.
func cleanText(s string) string {
	s = normalizeEOL(s)
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "\uFFFD")
	}
	return s
}

// virtualDoc returns the open document of id when it is a virtual buffer.
func (p *pluginEditor) virtualDoc(id pluginapi.BufferID) (*editor.Document, error) {
	d, err := p.doc(id)
	if err != nil {
		return nil, err
	}
	if !d.Virtual() {
		return nil, pluginapi.Errf(pluginapi.CodeInvalid, "buffer %d is not a virtual buffer", id)
	}
	return d, nil
}

// sweepVirtual drops the content styles of virtual buffers that are closed. Buffer ids are
// never reused, so nothing else would free them.
func (p *pluginEditor) sweepVirtual() {
	for _, doc := range p.decor.Docs() {
		if _, _, ok := p.ed.DocByID(editor.DocID(doc)); !ok {
			p.decor.DropDoc(doc)
		}
	}
}

// CreateBuffer opens a virtual buffer as the last tab, and makes it the active one when
// o.Focus is set.
func (p *pluginEditor) CreateBuffer(o pluginapi.CreateBuffer) (pluginapi.BufferID, error) {
	switch {
	case strings.TrimSpace(o.Name) == "":
		return 0, pluginapi.Errf(pluginapi.CodeInvalid, "a virtual buffer needs a name")
	case len(o.Text) > editor.MaxVirtualText:
		return 0, pluginapi.Errf(pluginapi.CodeTooLarge, "text is over %d bytes", editor.MaxVirtualText)
	case o.Language != "" && !knownLanguage(o.Language):
		return 0, unknownLanguage(o.Language)
	case len(o.Mode) > pluginMaxModeName:
		return 0, pluginapi.Errf(pluginapi.CodeInvalid, "mode name is longer than %d bytes", pluginMaxModeName)
	}
	p.sweepVirtual()
	text := cleanText(o.Text)
	if err := p.virtualRoom(o.Plugin, true, 0, len(text)); err != nil {
		return 0, err
	}
	id := p.ed.NewVirtual(editor.VirtualOptions{
		Name: o.Name, Language: o.Language, Mode: o.Mode, ReadOnly: o.ReadOnly, Text: text, Owner: o.Plugin,
	})
	if o.Focus {
		_ = p.ed.ActivateID(id)
	}
	return pluginapi.BufferID(id), nil
}

// virtualRoom checks the quotas of virtual buffers for owner: a new buffer (add) or the
// replacement of the text of buffer replace, which is size bytes long afterwards, must
// fit the buffer count and the text budget of the owner and of the editor.
func (p *pluginEditor) virtualRoom(owner string, add bool, replace editor.DocID, size int) error {
	freed := 0
	if !add {
		_, freed, _ = p.ed.VirtualOwner(replace)
	}
	n, bytes := p.ed.VirtualUsage(owner)
	allN, allBytes := p.ed.VirtualTotals()
	switch {
	case add && n >= pluginMaxVirtual:
		return pluginapi.Errf(pluginapi.CodeLimit, "too many virtual buffers (max %d per plugin)", pluginMaxVirtual)
	case add && allN >= pluginMaxVirtualAll:
		return pluginapi.Errf(pluginapi.CodeLimit, "too many virtual buffers in the editor (max %d)", pluginMaxVirtualAll)
	case bytes-freed+size > pluginMaxVirtualBytes:
		return pluginapi.Errf(pluginapi.CodeLimit, "virtual buffers of a plugin hold over %d bytes of text", pluginMaxVirtualBytes)
	case allBytes-freed+size > pluginMaxVirtualBytesAll:
		return pluginapi.Errf(pluginapi.CodeLimit, "virtual buffers hold over %d bytes of text", pluginMaxVirtualBytesAll)
	}
	return nil
}

// SetReadOnly sets the read-only flag of a virtual buffer.
func (p *pluginEditor) SetReadOnly(id pluginapi.BufferID, ro bool) error {
	if _, err := p.virtualDoc(id); err != nil {
		return err
	}
	return editorErr(p.ed.SetReadOnlyID(editor.DocID(id), ro))
}

// textPos is a position in the content being built: a line and a UTF-8 byte column.
type textPos struct{ line, col int }

// after returns the position after s is written at p.
func (p textPos) after(s string) textPos {
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return textPos{p.line + strings.Count(s, "\n"), len(s) - i - 1}
	}
	return textPos{p.line, p.col + len(s)}
}

// contentBuilder turns the entries of a SetContent call into the text, the style items
// and the property spans of the buffer.
type contentBuilder struct {
	text  strings.Builder
	at    textPos
	items []decorations.Item
	props []editor.PropSpan
}

// full is the error of a content that does not fit: bytes of text or styled runs.
func (c *contentBuilder) full() error {
	switch {
	case c.text.Len() > editor.MaxVirtualText:
		return pluginapi.Errf(pluginapi.CodeTooLarge, "content is over %d bytes", editor.MaxVirtualText)
	case len(c.items) > decorations.MaxPerNamespace:
		return pluginapi.Errf(pluginapi.CodeLimit, "too many styled runs in the content (max %d)", decorations.MaxPerNamespace)
	}
	return nil
}

// put appends s at the current position.
func (c *contentBuilder) put(s string) {
	c.text.WriteString(s)
	c.at = c.at.after(s)
}

// style styles text, which was written at start, with st unless st is the zero style. The
// trailing line break is left unstyled, so a background does not leak onto the start of
// the next line.
func (c *contentBuilder) style(start textPos, text string, st pluginapi.Style, priority int) {
	if st == (pluginapi.Style{}) {
		return
	}
	end := start.after(strings.TrimSuffix(text, "\n"))
	if end == start {
		return
	}
	c.items = append(c.items, decorations.Item{
		Kind: decorations.Highlight, FromLine: start.line, FromCol: start.col, ToLine: end.line, ToCol: end.col,
		Style: contentStyle(st), Priority: priority,
	})
}

// entry adds one entry. Segments, when there are any, are the entry's text, and the style
// of the entry lies under theirs.
func (c *contentBuilder) entry(e pluginapi.ContentEntry) error {
	start, offset := c.at, c.text.Len()
	if len(e.Segments) == 0 {
		c.put(cleanText(e.Text))
	}
	for _, seg := range e.Segments {
		text, at := cleanText(seg.Text), c.at
		c.put(text)
		c.style(at, text, seg.Style, virtualSegmentPriority)
		if err := c.full(); err != nil {
			return err
		}
	}
	c.style(start, c.text.String()[offset:], e.Style, 0)
	if err := c.full(); err != nil {
		return err
	}
	if len(e.Props) > 0 && c.at != start {
		c.props = append(c.props, editor.PropSpan{
			FromLine: start.line, FromCol: start.col, ToLine: c.at.line, ToCol: c.at.col, Props: e.Props,
		})
	}
	return nil
}

// contentStyle converts the style of a content run to a decoration style.
func contentStyle(s pluginapi.Style) decorations.Style {
	color := func(c pluginapi.Color) decorations.Color {
		return decorations.Color{Key: c.Key, R: c.R, G: c.G, B: c.B, Set: c.Set}
	}
	return decorations.Style{
		FG: color(s.FG), BG: color(s.BG),
		Bold: s.Bold, Italic: s.Italic, Underline: s.Underline, Strike: s.Strike, Reverse: s.Reverse, Extend: s.Extend,
	}
}

// SetContent replaces the text, the styles and the properties of a virtual buffer with the
// entries, concatenated as given, all in one step: a content that does not fit changes
// nothing.
func (p *pluginEditor) SetContent(id pluginapi.BufferID, entries []pluginapi.ContentEntry) error {
	d, err := p.virtualDoc(id)
	if err != nil {
		return err
	}
	var c contentBuilder
	for _, e := range entries {
		if err := c.entry(e); err != nil {
			return err
		}
	}
	doc := editor.DocID(id)
	owner, _, _ := p.ed.VirtualOwner(doc)
	if err := p.virtualRoom(owner, false, doc, c.text.Len()); err != nil {
		return err
	}
	if err := p.ed.SetVirtualText(doc, c.text.String()); err != nil {
		return editorErr(err)
	}
	p.sweepVirtual()
	err = p.ed.SetVirtualProps(doc, c.props)
	if err == nil {
		err = p.decor.Set(uint64(id), virtualOwner(id), virtualNS, c.items)
	}
	p.markEdited(d)
	if errors.Is(err, decorations.ErrFull) {
		return pluginapi.Errf(pluginapi.CodeLimit, "too many styled runs in the content")
	}
	return editorErr(err)
}

// PropsAt returns the properties of the content run that covers p. A buffer that is not
// virtual, or was edited since its content was set, has none.
func (p *pluginEditor) PropsAt(id pluginapi.BufferID, pos pluginapi.Pos) (map[string]any, error) {
	d, err := p.doc(id)
	if err != nil {
		return nil, err
	}
	if !d.Virtual() {
		return nil, nil
	}
	props, err := p.ed.VirtualProps(editor.DocID(id), max(pos.Line, 0), max(pos.Col, 0))
	return props, editorErr(err)
}

// SetBufferMode sets the buffer-local input mode of a buffer; "" clears it.
func (p *pluginEditor) SetBufferMode(id pluginapi.BufferID, mode string) error {
	if len(mode) > pluginMaxModeName {
		return pluginapi.Errf(pluginapi.CodeInvalid, "mode name is longer than %d bytes", pluginMaxModeName)
	}
	return editorErr(p.ed.SetModeID(editor.DocID(id), mode))
}

// The options of a buffer: its own override of the display settings. A value of nil
// removes the override, so the buffer follows the global setting again.
const (
	optWrap        = "wrap"
	optLineNumbers = "line_numbers"
	optTabSize     = "tab_size"
)

// SetOption sets an option of one buffer: "wrap" and "line_numbers" take a boolean,
// "tab_size" a whole number from 1 to editor.MaxViewTabSize; nil clears the override.
func (p *pluginEditor) SetOption(id pluginapi.BufferID, name string, v any) error {
	var set func(*editor.ViewSettings)
	switch name {
	case optWrap, optLineNumbers:
		var flag *bool
		if v != nil {
			b, ok := v.(bool)
			if !ok {
				return pluginapi.Errf(pluginapi.CodeInvalid, "option %q takes a boolean", name)
			}
			flag = &b
		}
		set = func(s *editor.ViewSettings) {
			if name == optWrap {
				s.LineWrap = flag
			} else {
				s.LineNumbers = flag
			}
		}
	case optTabSize:
		size := 0
		if v != nil {
			n, ok := wholeValue(v)
			if !ok || n < 1 || n > editor.MaxViewTabSize {
				return pluginapi.Errf(pluginapi.CodeInvalid, "option %q takes a whole number from 1 to %d", name, editor.MaxViewTabSize)
			}
			size = n
		}
		set = func(s *editor.ViewSettings) { s.TabSize = size }
	default:
		return pluginapi.Errf(pluginapi.CodeInvalid, "unknown option %q (wrap, line_numbers, tab_size)", name)
	}
	return editorErr(p.ed.UpdateViewSettings(editor.DocID(id), set))
}

// GetOption returns the override a buffer has for an option, or nil when it follows the
// global setting.
func (p *pluginEditor) GetOption(id pluginapi.BufferID, name string) (any, error) {
	d, err := p.doc(id)
	if err != nil {
		return nil, err
	}
	s := d.ViewSettings()
	flag := func(f *bool) (any, error) {
		if f == nil {
			return nil, nil
		}
		return *f, nil
	}
	switch name {
	case optWrap:
		return flag(s.LineWrap)
	case optLineNumbers:
		return flag(s.LineNumbers)
	case optTabSize:
		if s.TabSize == 0 {
			return nil, nil
		}
		return s.TabSize, nil
	}
	return nil, pluginapi.Errf(pluginapi.CodeInvalid, "unknown option %q (wrap, line_numbers, tab_size)", name)
}

// wholeValue reads a whole number from a value the plugin runtime produced (int64 or
// float64).
func wholeValue(v any) (int, bool) {
	switch x := v.(type) {
	case int64:
		return int(x), x == int64(int(x))
	case float64:
		n := int(x)
		return n, float64(n) == x
	case int:
		return x, true
	}
	return 0, false
}
