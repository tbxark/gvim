package tui

import (
	"errors"
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/tbxark/gvim/decorations"
	"github.com/tbxark/gvim/fold"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/theme"
)

// Plugin decorations: the pluginHost methods that put
// them in the decorations.Store, and the hooks that hand them to the pane one line at a time.
//
// The store is fed with the edits of every document by a listener that the app registers on
// the editor (see the decorSource below), so by the time an edit returns the anchors have
// moved and a plugin that reads positions afterwards computes them against the moved
// anchors. Nothing here scans a document: the pane asks for the lines it paints, and the
// store answers from a small window of lines.

// decorSource is implemented by the plugin editor port (RootPorts.PluginEditor) of the app.
// It names the store the editor's change listener feeds, so the root reads the same store
// the edits move. A port without it gets a store of its own, which no edit feeds.
type decorSource interface {
	DecorStore() *decorations.Store
}

// bufferIdentified is implemented by a Document that is a plugin buffer: it names the
// BufferID the plugins know it by. A Document without it shows no decorations.
type bufferIdentified interface {
	BufferID() pluginapi.BufferID
}

// decorStoreOf returns the store of the plugin editor port, or a new one.
func decorStoreOf(pe PluginEditor) *decorations.Store {
	if s, ok := pe.(decorSource); ok {
		if st := s.DecorStore(); st != nil {
			return st
		}
	}
	return decorations.New()
}

const (
	// decorMaxText bounds the text of one virtual-text or gutter item.
	decorMaxText = 4096
	// decorColEnd is the column from which a position means "the end of its line". The
	// host resolves it, so the anchors never hold an absurd column.
	decorColEnd = 1 << 30
)

// localBuffer resolves a plugin API buffer id to the active workspace's local id.
func (h *pluginHost) localBuffer(id pluginapi.BufferID) (pluginapi.BufferID, error) {
	return h.model().pluginResolveBuffer(id)
}

// ---- pluginapi.DecorHost

// decorTarget checks that buffer id is open and, when expect is set, that its text is the
// one the plugin computed its positions against.
func (h *pluginHost) decorTarget(id pluginapi.BufferID, expect *pluginapi.Version) (pluginapi.BufferInfo, error) {
	if h.model().plug.decor == nil {
		return pluginapi.BufferInfo{}, pluginapi.Errf(pluginapi.CodeUnsupported, "decorations are not available")
	}
	info, ok := h.ed().Buffer(id)
	if !ok {
		return info, pluginapi.Errf(pluginapi.CodeClosed, "buffer %d is closed", id)
	}
	if expect != nil && *expect != info.Version {
		return info, pluginapi.Errf(pluginapi.CodeStale, "buffer %d changed: version %s, expected %s", id, info.Version, *expect)
	}
	return info, nil
}

// DecorSet replaces everything the namespace has on the buffer.
func (h *pluginHost) DecorSet(plugin, ns string, id pluginapi.BufferID, items []pluginapi.Decoration, expect *pluginapi.Version) error {
	local, err := h.localBuffer(id)
	if err != nil {
		return err
	}
	info, err := h.decorTarget(id, expect)
	if err != nil {
		return err
	}
	conv := make([]decorations.Item, len(items))
	for i, it := range items {
		if conv[i], err = decorItem(h, local, info, it); err != nil {
			return pluginapi.Errf(pluginapi.CodeInvalid, "item %d: %v", i+1, err)
		}
	}
	err = h.model().plug.decor.Set(uint64(local), plugin, ns, conv)
	h.decorSweep()
	return decorErr(err)
}

// DecorAdd adds one item to the namespace and returns its id.
func (h *pluginHost) DecorAdd(plugin, ns string, id pluginapi.BufferID, item pluginapi.Decoration, expect *pluginapi.Version) (int, error) {
	local, err := h.localBuffer(id)
	if err != nil {
		return 0, err
	}
	info, err := h.decorTarget(id, expect)
	if err != nil {
		return 0, err
	}
	it, err := decorItem(h, local, info, item)
	if err != nil {
		return 0, pluginapi.Errf(pluginapi.CodeInvalid, "%v", err)
	}
	n, err := h.model().plug.decor.Add(uint64(local), plugin, ns, it)
	h.decorSweep()
	return n, decorErr(err)
}

// DecorRemove removes one item.
func (h *pluginHost) DecorRemove(plugin, ns string, id pluginapi.BufferID, item int) {
	local, err := h.localBuffer(id)
	if err != nil {
		return
	}
	if st := h.model().plug.decor; st != nil {
		st.Remove(uint64(local), plugin, ns, item)
		h.decorSweep()
	}
}

// DecorClearRange removes the namespace's items that intersect the line range.
func (h *pluginHost) DecorClearRange(plugin, ns string, id pluginapi.BufferID, fromLine, toLine int) {
	local, err := h.localBuffer(id)
	if err != nil {
		return
	}
	if st := h.model().plug.decor; st != nil {
		st.ClearRange(uint64(local), plugin, ns, fromLine, toLine)
		h.decorSweep()
	}
}

// DecorRefreshLines is a no-op: painting reads the store every frame.
func (h *pluginHost) DecorRefreshLines(pluginapi.BufferID) {}

// DecorClear removes everything the namespace has on the buffer; buffer 0 means every
// buffer.
func (h *pluginHost) DecorClear(plugin, ns string, id pluginapi.BufferID) {
	local := pluginapi.BufferID(0)
	if id != 0 {
		var err error
		if local, err = h.localBuffer(id); err != nil {
			return
		}
	}
	if st := h.model().plug.decor; st != nil {
		st.Clear(uint64(local), plugin, ns)
		h.decorSweep()
	}
}

// plugDecorGone drops the decorations and plugin folds that the plugin set. A reload
// starts a new generation, and the new one sets what it needs again.
func (m *Model) plugDecorGone(plugin string) {
	if m.plug.decor != nil {
		m.plug.decor.DropOwner(plugin)
	}
	m.plug.folds.dropOwner(plugin)
}

func decorErr(err error) error {
	if errors.Is(err, decorations.ErrFull) {
		return pluginapi.Errf(pluginapi.CodeLimit, "too many decorations (max %d per namespace, %d per plugin)", decorations.MaxPerNamespace, decorations.MaxPerOwner)
	}
	return err
}

// decorSweep asks for the decorations of closed buffers to be forgotten. The sweep reads
// every decorated document and flushes the store, so it runs once at the end of the update
// (plugDecorSweep), not after each of the many calls one slice can make.
func (h *pluginHost) decorSweep() {
	h.model().plug.decorSweepDue = true
}

// plugDecorSweep forgets the decorations of buffers that are closed, when a decoration
// call asked for it. Buffer ids are never reused, so nothing else would ever free them.
func (m *Model) plugDecorSweep() {
	p := &m.plug
	if !p.decorSweepDue || p.decor == nil {
		return
	}
	p.decorSweepDue = false
	for _, doc := range p.decor.Docs() {
		if _, ok := p.host.ed().Buffer(pluginapi.BufferID(doc)); !ok {
			p.decor.DropDoc(doc)
		}
	}
}

// decorItem converts and checks one decoration of the API. A line past the end of the
// document is the last line; a column is left as it is, except the "end of line" column,
// which is resolved against the line.
func decorItem(h *pluginHost, id pluginapi.BufferID, info pluginapi.BufferInfo, d pluginapi.Decoration) (decorations.Item, error) {
	last := math.MaxInt
	if info.LineCount > 0 {
		last = info.LineCount - 1
	}
	line := func(l int) int { return min(max(l, 0), last) }
	pos := func(p pluginapi.Pos) (int, int) {
		l, c := line(p.Line), max(p.Col, 0)
		if p.Line > last {
			c = decorColEnd // past the document: the end of it
		}
		if c >= decorColEnd {
			c = h.lineEnd(id, l)
		}
		return l, c
	}
	it := decorations.Item{
		Style:       decorStyle(d.Style),
		Color:       decorColor(d.Color),
		Priority:    d.Priority,
		Activation:  decorations.Activation(d.Activation),
		ComposeOnly: d.ComposeOnly,
		Indent:      d.Indent,
		PrefixText:  d.PrefixText,
		PrefixStyle: decorStyle(d.PrefixStyle),
		GutterText:  d.GutterText,
		GutterStyle: decorStyle(d.GutterStyle),
	}
	if d.ScopeEnd >= d.ScopeStart {
		it.ScopeStart, it.ScopeEnd = line(d.ScopeStart), line(d.ScopeEnd)
	} else {
		it.ScopeStart, it.ScopeEnd = 0, -1
	}
	switch d.Kind {
	case pluginapi.DecorHighlight:
		it.Kind = decorations.Highlight
		it.FromLine, it.FromCol = pos(d.Range.From)
		it.ToLine, it.ToCol = pos(d.Range.To)
	case pluginapi.DecorLine:
		it.Kind, it.FromLine = decorations.Line, line(d.Line)
	case pluginapi.DecorEOL:
		it.Kind, it.FromLine, it.Text = decorations.EOL, line(d.Line), d.Text
	case pluginapi.DecorGutter:
		it.Kind, it.FromLine, it.Text = decorations.Gutter, line(d.Line), d.Text
	case pluginapi.DecorScroll:
		it.Kind = decorations.Scroll
		if d.Range.From != d.Range.To {
			it.FromLine, it.FromCol = pos(d.Range.From)
			it.ToLine, it.ToCol = pos(d.Range.To)
			it.FromCol = 0
		} else {
			it.FromLine = line(d.Line)
		}
	case pluginapi.DecorReplace:
		it.Kind, it.Text = decorations.Replace, d.Text
		it.FromLine, it.FromCol = pos(d.Pos)
	case pluginapi.DecorVirtualLineAbove, pluginapi.DecorVirtualLineBelow:
		it.Kind, it.FromLine, it.Text = decorations.VirtualLineAbove, line(d.Line), d.Text
		if d.Kind == pluginapi.DecorVirtualLineBelow {
			it.Kind = decorations.VirtualLineBelow
		}
		it.GutterText = d.GutterText
		for i, ov := range d.TextOverlays {
			if i >= decorations.MaxTextOverlaysPerLine() {
				break
			}
			from, to := max(ov.Range.From.Col, 0), max(ov.Range.To.Col, 0)
			if to < from {
				from, to = to, from
			}
			it.TextOverlays[i] = decorations.TextOverlay{Start: from, End: to, Style: decorStyle(ov.Style)}
			it.TextOverlayCount = i + 1
		}
	case pluginapi.DecorConceal:
		it.Kind, it.Text = decorations.Conceal, d.Text
		it.FromLine, it.FromCol = pos(d.Range.From)
		it.ToLine, it.ToCol = pos(d.Range.To)
	case pluginapi.DecorSoftBreak:
		it.Kind = decorations.SoftBreak
		it.FromLine, it.FromCol = pos(d.Pos)
	case pluginapi.DecorCursorLine:
		// The caret line is dynamic: one overlay per buffer, kept on line 0.
		it.Kind, it.FromLine, it.FromCol = decorations.CursorLine, 0, 0
	default:
		return it, fmt.Errorf("unknown decoration kind %d", d.Kind)
	}
	if len(it.Text) > decorMaxText {
		return it, fmt.Errorf("text is longer than %d bytes", decorMaxText)
	}
	return it, nil
}

// lineEnd is the byte length of a line of the buffer: the column of its end.
func (h *pluginHost) lineEnd(id pluginapi.BufferID, line int) int {
	ls, err := h.ed().Lines(id, line, 1)
	if err != nil || len(ls) == 0 {
		return 0
	}
	return len(ls[0])
}

func decorColor(c pluginapi.Color) decorations.Color {
	return decorations.Color{Key: c.Key, R: c.R, G: c.G, B: c.B, Set: c.Set}
}

func decorStyle(s pluginapi.Style) decorations.Style {
	return decorations.Style{
		FG: decorColor(s.FG), BG: decorColor(s.BG),
		Bold: s.Bold, Italic: s.Italic, Underline: s.Underline, Strike: s.Strike, Reverse: s.Reverse, Extend: s.Extend,
	}
}

// ---- painting

// plugDecor gives the pane the hooks that paint the decorations of doc. A document without
// decorations, and a root with no plugins, get the pane back untouched. Building the hooks
// only captures the store, so the geometry paths that build a pane per call pay nothing.
func (m *Model) plugDecor(p editorpane.Pane, doc Document) editorpane.Pane {
	// Plugin folds are independent of the decoration store: wire them even when no
	// decorations are set.
	if len(m.plug.folds.byBuffer) > 0 {
		if bd, ok := doc.(bufferIdentified); ok {
			if len(m.plug.folds.byBuffer[pluginapi.BufferID(bd.BufferID())]) > 0 {
				p.PluginFolds = func() ([]fold.Range, map[int]bool, map[int]string) { return m.plugFolds(doc) }
			}
		}
	}
	st := m.plug.decor
	if st == nil || st.Empty() {
		return p
	}
	bd, ok := doc.(bufferIdentified)
	if !ok {
		return p
	}
	id := uint64(bd.BufferID())
	if st.Count(id) == 0 {
		return p
	}
	v := &decorView{st: st, id: id, doc: doc, th: m.theme, compose: p.PageView.On}
	p.Overlays, p.RowStyle, p.EOL = v.spans, v.rowStyle, v.eol
	p.Replaces, p.Gutter, p.ScrollMarks = v.replaces, v.gutter, v.scrollMark
	p.Conceals, p.CursorLineOverlay = v.conceals, v.cursorLine
	p.ExtraRows, p.SoftBreaks = v.extraRows, v.softBreaks
	if len(m.plug.folds.byBuffer) > 0 {
		p.PluginFolds = func() ([]fold.Range, map[int]bool, map[int]string) { return m.plugFolds(doc) }
	}
	return p
}

// decorView answers the hooks of one pane from the store. It remembers the text of the
// last line it converted columns on.
type decorView struct {
	st  *decorations.Store
	id  uint64
	doc Document
	th  theme.Theme
	// compose is the pane's composing state: tier-1c items marked compose_only show only
	// there (the page view).
	compose bool

	text     string
	textLine int
	haveText bool
	long     bool // the line is too long to read: byte columns stand for rune columns
	ascii    bool // the text is ASCII: a byte column is a rune column
}

// lineText returns the text of the line the byte columns are converted on. A line of a
// large document that is painted through a window is not read: its columns are taken as
// they are, which is exact for ASCII.
func (v *decorView) lineText(line int) (string, bool) {
	if v.haveText && v.textLine == line {
		return v.text, !v.long
	}
	v.haveText, v.textLine, v.long, v.text, v.ascii = true, line, false, "", false
	if line < 0 || line >= v.doc.LineCount() {
		return "", false
	}
	if lw, ok := v.doc.(editorpane.LineWindower); ok && lw.LineBytes(line) > editorpane.LongLineRunes {
		v.long = true
		return "", false
	}
	v.text = v.doc.Line(line)
	v.ascii = isASCII(v.text)
	return v.text, true
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// col converts a byte column of the line to a rune column.
func (v *decorView) col(line, byteCol int) int {
	text, ok := v.lineText(line)
	if !ok {
		return byteCol
	}
	if v.ascii {
		return min(max(byteCol, 0), len(text))
	}
	return runeCol(text, byteCol)
}

// runeCol is the number of whole runes of text before byte column b: a column in the middle
// of a rune is that rune's start, and a column past the end is the end.
func runeCol(text string, b int) int {
	if b >= len(text) {
		return utf8.RuneCountInString(text)
	}
	n := 0
	for i := 0; i < len(text); n++ {
		_, size := utf8.DecodeRuneInString(text[i:])
		if i+size > b {
			break
		}
		i += size
	}
	return n
}

func (v *decorView) spans(line int) []editorpane.StyledSpan {
	ld := v.st.Line(v.id, line)
	if len(ld.Spans) == 0 {
		return nil
	}
	out := make([]editorpane.StyledSpan, len(ld.Spans))
	for i, s := range ld.Spans {
		end := -1
		if s.End >= 0 {
			end = v.col(line, s.End)
		}
		out[i] = editorpane.StyledSpan{
			Start: v.col(line, s.Start), End: end,
			Style: style.DecorStyle(v.th, s.Style), Extend: s.Style.Extend, Priority: s.Priority,
		}
	}
	return out
}

func (v *decorView) rowStyle(line int) (grid.Style, bool) {
	ld := v.st.Line(v.id, line)
	if len(ld.Rows) == 0 {
		return grid.Style{}, false
	}
	var st grid.Style
	for _, r := range ld.Rows {
		st = editorpane.PatchStyle(st, style.DecorStyle(v.th, r.Style))
	}
	return st, true
}

func (v *decorView) eol(line int) []editorpane.VirtualText {
	ld := v.st.Line(v.id, line)
	if len(ld.EOL) == 0 {
		return nil
	}
	out := make([]editorpane.VirtualText, len(ld.EOL))
	for i, t := range ld.EOL {
		out[i] = editorpane.VirtualText{Text: t.Text, Style: style.DecorStyle(v.th, t.Style)}
	}
	return out
}

func (v *decorView) replaces(line int) []editorpane.Replacement {
	ld := v.st.Line(v.id, line)
	if len(ld.Replaces) == 0 {
		return nil
	}
	out := make([]editorpane.Replacement, len(ld.Replaces))
	for i, r := range ld.Replaces {
		out[i] = editorpane.Replacement{Col: v.col(line, r.Col), Text: r.Text, Style: style.DecorStyle(v.th, r.Style)}
	}
	return out
}

func (v *decorView) gutter(line int) (editorpane.GutterMark, bool) {
	g := v.st.Line(v.id, line).Gutter
	if g == nil {
		return editorpane.GutterMark{}, false
	}
	return editorpane.GutterMark{Text: g.Text, Style: style.DecorStyle(v.th, g.Style)}, true
}

func (v *decorView) scrollMark(line int) (editorpane.ScrollMark, bool) {
	s := v.st.Line(v.id, line).Scroll
	if s == nil {
		return editorpane.ScrollMark{}, false
	}
	return editorpane.ScrollMark{Style: grid.Style{FG: style.ResolveColor(v.th, s.Color)}}, true
}

// conceals gives the pane the line's conceal spans (tier 1c-B).
func (v *decorView) conceals(line int) []editorpane.ConcealSpan {
	ld := v.st.Line(v.id, line)
	if len(ld.Conceals) == 0 {
		return nil
	}
	out := make([]editorpane.ConcealSpan, 0, len(ld.Conceals))
	for _, c := range ld.Conceals {
		if c.ComposeOnly && !v.compose {
			continue
		}
		end := -1
		if c.End >= 0 {
			end = v.col(line, c.End)
		}
		scopeStart, scopeEnd := c.Scope[0], c.Scope[1]
		out = append(out, editorpane.ConcealSpan{
			Start: v.col(line, c.Start), End: end,
			Text: c.Text, Style: style.DecorStyle(v.th, c.Style),
			Activation: editorpane.ConcealActivation(c.Activation),
			Scoped:     scopeEnd >= scopeStart,
			ScopeStart: scopeStart, ScopeEnd: scopeEnd,
		})
	}
	return out
}

// cursorLine gives the pane the caret row's overlay: the highest priority item wins.
func (v *decorView) cursorLine() (grid.Style, bool) {
	// The caret line is dynamic, so the host keeps the item on line 0.
	ld := v.st.Line(v.id, 0)
	if len(ld.CursorLine) == 0 {
		return grid.Style{}, false
	}
	var st grid.Style
	found := false
	for _, c := range ld.CursorLine {
		if c.ComposeOnly && !v.compose {
			continue
		}
		st = editorpane.PatchStyle(st, style.DecorStyle(v.th, c.Style))
		found = true
	}
	return st, found
}

// extraRows gives the pane the line's virtual rows above and below (tier 1c-B). Items
// marked compose_only show only in a composing pane.
func (v *decorView) extraRows(line int) editorpane.ExtraRows {
	ld := v.st.Line(v.id, line)
	if len(ld.Above) == 0 && len(ld.Below) == 0 {
		return editorpane.ExtraRows{}
	}
	conv := func(rows []decorations.VirtualRow) []editorpane.ExtraRow {
		out := make([]editorpane.ExtraRow, 0, len(rows))
		for _, r := range rows {
			if r.ComposeOnly && !v.compose {
				continue
			}
			gutterStyle := style.DecorStyle(v.th, r.GutterStyle)
			row := editorpane.ExtraRow{
				Text: r.Text, Style: style.DecorStyle(v.th, r.Style),
				GutterText: r.GutterText, GutterStyle: gutterStyle, Priority: r.Priority,
			}
			for _, ov := range r.Overlays {
				row.Overlays = append(row.Overlays, editorpane.ExtraTextOverlay{
					Start: ov.Start, End: ov.End, Style: style.DecorStyle(v.th, ov.Style),
				})
			}
			out = append(out, row)
		}
		return out
	}
	return editorpane.ExtraRows{Above: conv(ld.Above), Below: conv(ld.Below)}
}

// softBreaks gives the pane the line's soft breaks (tier 1c-B).
func (v *decorView) softBreaks(line int) []editorpane.SoftBreak {
	ld := v.st.Line(v.id, line)
	if len(ld.SoftBreaks) == 0 {
		return nil
	}
	out := make([]editorpane.SoftBreak, 0, len(ld.SoftBreaks))
	for _, b := range ld.SoftBreaks {
		if b.ComposeOnly && !v.compose {
			continue
		}
		scopeStart, scopeEnd := b.Scope[0], b.Scope[1]
		out = append(out, editorpane.SoftBreak{
			Col: v.col(line, b.Col), Indent: b.Indent, PrefixText: b.PrefixText,
			PrefixStyle: style.DecorStyle(v.th, b.PrefixStyle), Priority: b.Priority,
			Activation: editorpane.ConcealActivation(b.Activation),
			Scoped:     scopeEnd >= scopeStart, ScopeStart: scopeStart, ScopeEnd: scopeEnd,
		})
	}
	return out
}
