package pluginhost

import (
	"errors"
	"path/filepath"

	"github.com/alecthomas/chroma/v2/lexers"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/decorations"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/search"
	"github.com/tbxark/gvim/syntax"
)

// Limits of one Edit call (AMENDMENTS C18): the edits, and the bytes they insert plus
// remove. The runtime checks the inserted half and the count before it calls; the removed
// half is only known here, where the ranges meet the text.
const (
	pluginMaxEdits     = 50_000
	pluginMaxEditBytes = 8 << 20
	// pluginLangCache bounds the file names whose detected language is remembered.
	pluginLangCache = 1024
)

// pluginEditor is the editor as the plugin host sees it: pluginapi.BufferEditor over
// *editor.Editor, in API coordinates (0-based lines, 0-based UTF-8 byte columns; the
// buffer's own positions are rune columns). It is a separate type from editorAdapter
// because the two ports have SetCursor methods of different signatures.
//
// Every mutating method marks the syntax spans of the document it changed as stale
// (editorAdapter.editedDoc), which also covers a document that is not the active tab:
// a split pane can show it. The virtual-buffer methods are in pluginvirtual.go.
type pluginEditor struct {
	pluginapi.Unsupported
	ed *editor.Editor
	// markEdited marks the syntax spans of a document stale for an edit made here.
	markEdited func(*editor.Document)
	// langs remembers the language detected from a file name, so ListBuffers does not
	// match the name against every lexer on each call.
	langs map[string]string
	// decor holds the decorations of the plugins; the editor's change listener moves them.
	decor *decorations.Store
}

var _ pluginapi.BufferEditor = (*pluginEditor)(nil)

func newPluginEditor(ed *editor.Editor, markEdited func(*editor.Document)) *pluginEditor {
	p := &pluginEditor{ed: ed, markEdited: markEdited, langs: map[string]string{}}
	p.trackDecorations()
	return p
}

// ---------- errors and positions ----------

// editorErr maps an error of the editor, the buffer or the search package to the plugin
// error with the code Lua sees.
func editorErr(err error) error {
	var pe *pluginapi.Error
	switch {
	case err == nil, errors.As(err, &pe):
		return err
	case errors.Is(err, editor.ErrNotOpen):
		return pluginapi.Errf(pluginapi.CodeClosed, "%v", err)
	case errors.Is(err, editor.ErrReadOnly):
		return pluginapi.Errf(pluginapi.CodeReadOnly, "%v", err)
	case errors.Is(err, editor.ErrStale):
		return pluginapi.Errf(pluginapi.CodeStale, "%v", err)
	case errors.Is(err, editor.ErrOverlappingEdits):
		return pluginapi.Errf(pluginapi.CodeInvalid, "overlapping edits")
	case errors.Is(err, editor.ErrRestoring), errors.Is(err, editor.ErrUnsaved), errors.Is(err, editor.ErrChangedOnDisk):
		return pluginapi.Errf(pluginapi.CodeConflict, "%v", err)
	case errors.Is(err, editor.ErrIndexing), errors.Is(err, buffer.ErrIndexPending):
		return pluginapi.Errf(pluginapi.CodeBusy, "%v", err)
	case errors.Is(err, search.ErrTooCostly):
		return pluginapi.Errf(pluginapi.CodeLimit, "%v", err)
	case errors.Is(err, editor.ErrLargeFile), errors.Is(err, buffer.ErrLargeBuffer), errors.Is(err, search.ErrLargeBuffer),
		errors.Is(err, search.ErrRangeTooLarge), errors.Is(err, buffer.ErrOverCap), errors.Is(err, buffer.ErrLineTooLong),
		errors.Is(err, buffer.ErrOverlayFull), errors.Is(err, buffer.ErrLineOverBudget):
		return pluginapi.Errf(pluginapi.CodeTooLarge, "%v", err)
	}
	return pluginapi.Errf(pluginapi.CodeInvalid, "%v", err)
}

func closedErr(id pluginapi.BufferID) error {
	return pluginapi.Errf(pluginapi.CodeClosed, "buffer %d is closed", id)
}

// bufPos converts an API position to the buffer position the rules of C24 give: a negative
// line is the first, a line at or past the end is the end of the document, a column past the
// end of the line is the line end, and a column inside a UTF-8 sequence rounds down to the
// start of its character. Positions are never an error.
func bufPos(b *buffer.Buffer, p pluginapi.Pos) buffer.Pos {
	if p.Line >= b.Lines() {
		return b.End()
	}
	return b.PosAtByteCol(max(p.Line, 0), max(p.Col, 0))
}

// apiPos is the API position of a buffer position.
func apiPos(b *buffer.Buffer, p buffer.Pos) pluginapi.Pos {
	return pluginapi.Pos{Line: p.Line, Col: b.ByteCol(p)}
}

// removedBytes returns the length in bytes of the text from..to (from <= to), counting each
// line break as one byte, but stops adding once the sum passes limit.
func removedBytes(b *buffer.Buffer, from, to buffer.Pos, limit int64) int64 {
	if from.Line == to.Line {
		return int64(b.ByteCol(to) - b.ByteCol(from))
	}
	// A large buffer knows from its index that the lines between are too many bytes: the sum
	// below would read them to find out.
	if n, over := b.LinesBytesOver(from.Line+1, to.Line-1, limit); over {
		return n
	}
	n := int64(b.LineBytes(from.Line)-b.ByteCol(from)) + 1
	for l := from.Line + 1; l < to.Line && n <= limit; l++ {
		n += int64(b.LineBytes(l)) + 1
	}
	return n + int64(b.ByteCol(to))
}

// doc returns the open document of a buffer id.
func (p *pluginEditor) doc(id pluginapi.BufferID) (*editor.Document, error) {
	d, _, ok := p.ed.DocByID(editor.DocID(id))
	if !ok {
		return nil, closedErr(id)
	}
	return d, nil
}

func versionOf(d *editor.Document) pluginapi.Version {
	v := d.Version()
	return pluginapi.Version{Gen: v.Gen, Ver: v.Ver}
}

// ---------- listing ----------

// ListBuffers lists the open documents in tab order.
func (p *pluginEditor) ListBuffers() []pluginapi.BufferInfo {
	ids := p.ed.DocIDs()
	active := p.ed.Active()
	out := make([]pluginapi.BufferInfo, len(ids))
	for i := range ids {
		d := p.ed.DocAt(i)
		out[i] = p.info(d, d == active)
	}
	return out
}

// Buffer describes one document.
func (p *pluginEditor) Buffer(id pluginapi.BufferID) (pluginapi.BufferInfo, bool) {
	d, err := p.doc(id)
	if err != nil {
		return pluginapi.BufferInfo{}, false
	}
	return p.info(d, d == p.ed.Active()), true
}

func (p *pluginEditor) info(d *editor.Document, active bool) pluginapi.BufferInfo {
	lines := d.Buffer.Lines()
	if d.Large() && !d.Index().Complete {
		lines = -1
	}
	return pluginapi.BufferInfo{
		ID: pluginapi.BufferID(d.ID()), Path: d.Path, Name: d.Name(), Language: p.language(d),
		Encoding: d.Encoding(), LineEnding: d.LineEnding(),
		Modified: d.Modified(), ReadOnly: d.ReadOnly(), Virtual: d.Virtual(), Large: d.Large(), Mode: d.Mode(),
		Preview: d.Preview(), Active: active,
		LineCount: lines, Version: versionOf(d),
	}
}

// language is the language set with Set Language, else the one the file name gives
// (lower case, as lexers.Get takes it), else "".
func (p *pluginEditor) language(d *editor.Document) string {
	if l := d.Language(); l != "" {
		return l
	}
	if d.Path == "" {
		return ""
	}
	if l, ok := p.langs[d.Path]; ok {
		return l
	}
	l := syntax.FileLanguage(d.Path)
	if len(p.langs) >= pluginLangCache {
		clear(p.langs)
	}
	p.langs[d.Path] = l
	return l
}

// pluginMaxLanguageName is longer than every lexer name and alias of chroma (27 bytes) and
// than any file name it recognises. lexers.Get matches the name against every file name glob,
// which costs time in proportion to its length, and a plugin's name is not bounded by the
// time budget (one host call), so longer names are refused before the lookup.
const pluginMaxLanguageName = 32

// knownLanguage reports whether a lexer answers to name.
func knownLanguage(name string) bool {
	return len(name) <= pluginMaxLanguageName && lexers.Get(name) != nil
}

// unknownLanguage is the error for a name knownLanguage refuses; it does not echo a long name.
func unknownLanguage(name string) error {
	if len(name) > pluginMaxLanguageName {
		name = name[:pluginMaxLanguageName] + "..."
	}
	return pluginapi.Errf(pluginapi.CodeInvalid, "unknown language %q", name)
}

// ActiveBuffer is the id of the active tab, or 0.
func (p *pluginEditor) ActiveBuffer() pluginapi.BufferID {
	return pluginapi.BufferID(p.ed.ActiveID())
}

// FindBuffer returns the id of the document whose file is path, or 0.
func (p *pluginEditor) FindBuffer(path string) pluginapi.BufferID {
	if path == "" {
		return 0
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	// by file, as the editor does when it opens a path: a symlink or another spelling of the
	// path of an open file names the tab that has it
	if i, ok := p.ed.DocumentIndexByPath(filepath.Clean(path)); ok {
		return pluginapi.BufferID(p.ed.DocAt(i).ID())
	}
	return 0
}

// ---------- reads ----------

// Lines returns count lines from first (to the end when count < 0). Lines past the end are
// not returned.
func (p *pluginEditor) Lines(id pluginapi.BufferID, first, count int) ([]string, error) {
	d, err := p.doc(id)
	if err != nil {
		return nil, err
	}
	b := d.Buffer
	n := b.Lines()
	first = max(first, 0)
	end := n
	if count >= 0 {
		end = min(n, first+count)
	}
	if first >= end {
		return nil, nil
	}
	out := make([]string, 0, end-first)
	for i := first; i < end; i++ {
		out = append(out, b.Line(i))
	}
	return out, nil
}

// Slice returns the text of r. A reversed range is taken in order.
func (p *pluginEditor) Slice(id pluginapi.BufferID, r pluginapi.Range) (string, error) {
	d, err := p.doc(id)
	if err != nil {
		return "", err
	}
	b := d.Buffer
	from, to := bufPos(b, r.From), bufPos(b, r.To)
	if to.Less(from) {
		from, to = to, from
	}
	if removedBytes(b, from, to, buffer.MaxMaterialize) > buffer.MaxMaterialize {
		return "", pluginapi.Errf(pluginapi.CodeTooLarge, "range is over %d bytes", buffer.MaxMaterialize)
	}
	s, err := b.RangeChecked(from, to)
	return s, editorErr(err)
}

// ByteLength is the length of the LF-joined text.
func (p *pluginEditor) ByteLength(id pluginapi.BufferID) (int, error) {
	d, err := p.doc(id)
	if err != nil {
		return 0, err
	}
	n, err := d.Buffer.ByteOffset(d.Buffer.End())
	return n, editorErr(err)
}

// OffsetOf converts a position to a byte offset in the LF-joined text.
func (p *pluginEditor) OffsetOf(id pluginapi.BufferID, pos pluginapi.Pos) (int, error) {
	d, err := p.doc(id)
	if err != nil {
		return 0, err
	}
	n, err := d.Buffer.ByteOffset(bufPos(d.Buffer, pos))
	return n, editorErr(err)
}

// PosOf converts a byte offset to a position; an offset outside the text is clamped.
func (p *pluginEditor) PosOf(id pluginapi.BufferID, offset int) (pluginapi.Pos, error) {
	d, err := p.doc(id)
	if err != nil {
		return pluginapi.Pos{}, err
	}
	pos, err := d.Buffer.PosAtByteOffset(offset)
	if err != nil {
		return pluginapi.Pos{}, editorErr(err)
	}
	return apiPos(d.Buffer, pos), nil
}

// FullText returns the whole text, LF-joined, and its version. A text over 16 MiB is too
// large.
func (p *pluginEditor) FullText(id pluginapi.BufferID) (string, pluginapi.Version, error) {
	d, err := p.doc(id)
	if err != nil {
		return "", pluginapi.Version{}, err
	}
	b := d.Buffer
	if !b.Large() {
		if n, err := b.ByteOffset(b.End()); err == nil && n > buffer.MaxMaterialize {
			return "", pluginapi.Version{}, pluginapi.Errf(pluginapi.CodeTooLarge, "text is over %d bytes", buffer.MaxMaterialize)
		}
	}
	text, err := b.TextChecked()
	if err != nil {
		return "", pluginapi.Version{}, editorErr(err)
	}
	return text, versionOf(d), nil
}

// SavedLines returns the text at open or the last save.
func (p *pluginEditor) SavedLines(id pluginapi.BufferID) ([]string, error) {
	d, err := p.doc(id)
	if err != nil {
		return nil, err
	}
	if d.Path == "" {
		return nil, pluginapi.Errf(pluginapi.CodeNotFound, "buffer %d has no saved text", id)
	}
	if d.Large() {
		return nil, pluginapi.Errf(pluginapi.CodeTooLarge, "the saved text of a large file is not held")
	}
	return d.SavedLines(), nil
}

// ---------- edits ----------

// Edit applies the edits as one undo step. Ranges are in the coordinates of the text
// before the call, and may come in any order, but must not overlap.
func (p *pluginEditor) Edit(id pluginapi.BufferID, edits []pluginapi.Edit, o pluginapi.EditOptions) error {
	d, err := p.doc(id)
	if err != nil {
		return err
	}
	if len(edits) > pluginMaxEdits {
		return pluginapi.Errf(pluginapi.CodeLimit, "too many edits (max %d)", pluginMaxEdits)
	}
	b := d.Buffer
	list := make([]editor.LSPEdit, len(edits))
	var total int64
	for i, e := range edits {
		from, to := bufPos(b, e.Range.From), bufPos(b, e.Range.To)
		if to.Less(from) {
			from, to = to, from
		}
		text := normalizeEOL(e.Text)
		total += int64(len(text)) + removedBytes(b, from, to, pluginMaxEditBytes)
		if total > pluginMaxEditBytes {
			return pluginapi.Errf(pluginapi.CodeLimit, "edit is over %d bytes (inserted plus removed)", pluginMaxEditBytes)
		}
		list[i] = editor.LSPEdit{From: from, To: to, Text: text}
	}
	opts := editor.EditOptions{Cursor: cursorMode(o.Cursor)}
	if o.Expect != nil {
		opts.Expect = &editor.Version{Gen: o.Expect.Gen, Ver: o.Expect.Ver}
	}
	err = p.ed.ApplyEdits(editor.DocID(id), list, opts)
	p.markEdited(d)
	if errors.Is(err, editor.ErrStale) {
		return pluginapi.Errf(pluginapi.CodeStale, "buffer %d changed (version %s, expected %s)", id, versionOf(d), o.Expect)
	}
	return editorErr(err)
}

func cursorMode(m pluginapi.CursorMode) editor.CursorMode {
	switch m {
	case pluginapi.CursorKeep:
		return editor.CursorKeep
	case pluginapi.CursorEnd:
		return editor.CursorEnd
	}
	return editor.CursorMap
}

// UndoGroup runs fn so that every edit it makes is one undo step.
func (p *pluginEditor) UndoGroup(id pluginapi.BufferID, fn func() error) error {
	d, err := p.doc(id)
	if err != nil {
		return err
	}
	err = p.ed.Group(editor.DocID(id), fn)
	p.markEdited(d)
	return err
}

// Undo reverts the newest undo group.
func (p *pluginEditor) Undo(id pluginapi.BufferID) error {
	return p.history(id, p.ed.UndoID, "undo")
}

// Redo reapplies the newest undone group.
func (p *pluginEditor) Redo(id pluginapi.BufferID) error {
	return p.history(id, p.ed.RedoID, "redo")
}

func (p *pluginEditor) history(id pluginapi.BufferID, step func(editor.DocID) (bool, error), what string) error {
	d, err := p.doc(id)
	if err != nil {
		return err
	}
	ok, err := step(editor.DocID(id))
	p.markEdited(d)
	if err != nil {
		return editorErr(err)
	}
	if !ok {
		return pluginapi.Errf(pluginapi.CodeNotFound, "nothing to %s", what)
	}
	return nil
}

// SetLanguage sets the language of the document; "" goes back to the one the file name
// gives.
func (p *pluginEditor) SetLanguage(id pluginapi.BufferID, lang string) error {
	d, err := p.doc(id)
	if err != nil {
		return err
	}
	if lang != "" && !knownLanguage(lang) {
		return unknownLanguage(lang)
	}
	err = p.ed.SetLanguageID(editor.DocID(id), lang)
	p.markEdited(d)
	return editorErr(err)
}

// ---------- carets ----------

// Cursors lists the carets, the primary one first.
func (p *pluginEditor) Cursors(id pluginapi.BufferID) ([]pluginapi.Caret, error) {
	d, err := p.doc(id)
	if err != nil {
		return nil, err
	}
	b := d.Buffer
	caret := func(head buffer.Pos, sel *buffer.Selection) pluginapi.Caret {
		c := pluginapi.Caret{Pos: apiPos(b, head)}
		if sel != nil && sel.Anchor != head {
			a := apiPos(b, sel.Anchor)
			c.Anchor = &a
		}
		return c
	}
	out := []pluginapi.Caret{caret(b.Clamp(d.Cursor), d.Sel)}
	for _, x := range d.Cursors() {
		out = append(out, caret(x.Cursor, x.Sel))
	}
	return out, nil
}

// SetCursor moves the primary caret. With extend the selection grows from its anchor.
// reveal scrolls the caret into view, and center puts its line in the middle of the pane.
func (p *pluginEditor) SetCursor(id pluginapi.BufferID, pos pluginapi.Pos, extend, reveal, center bool) error {
	d, err := p.doc(id)
	if err != nil {
		return err
	}
	if err := p.ed.SetCursorID(editor.DocID(id), bufPos(d.Buffer, pos), extend); err != nil {
		return editorErr(err)
	}
	if reveal || center {
		return editorErr(p.ed.RevealID(editor.DocID(id), center))
	}
	return nil
}

// SetSelection selects r with the caret at its end.
func (p *pluginEditor) SetSelection(id pluginapi.BufferID, r pluginapi.Range) error {
	d, err := p.doc(id)
	if err != nil {
		return err
	}
	return editorErr(p.ed.SelectID(editor.DocID(id), bufPos(d.Buffer, r.From), bufPos(d.Buffer, r.To)))
}

// ---------- search ----------

// Search finds the matches of a query in the buffer or in q.Within.
func (p *pluginEditor) Search(id pluginapi.BufferID, q pluginapi.SearchQuery) (pluginapi.SearchResult, error) {
	d, err := p.doc(id)
	if err != nil {
		return pluginapi.SearchResult{}, err
	}
	b := d.Buffer
	from, to := buffer.Pos{}, b.End()
	if q.Within != nil {
		from, to = bufPos(b, q.Within.From), bufPos(b, q.Within.To)
	}
	sq := search.Query{Text: q.Text, CaseSensitive: q.CaseSensitive, WholeWord: q.WholeWord, Regex: q.Regex}
	ms, truncated, err := search.FindRange(b, sq, from, to, q.Max)
	if err != nil {
		return pluginapi.SearchResult{}, editorErr(err)
	}
	res := pluginapi.SearchResult{Truncated: truncated}
	cols := b.NewByteColWalker()
	for _, m := range ms {
		if m.Start == m.End {
			continue
		}
		res.Ranges = append(res.Ranges, pluginapi.Range{
			From: pluginapi.Pos{Line: m.Start.Line, Col: cols.ByteCol(m.Start)},
			To:   pluginapi.Pos{Line: m.End.Line, Col: cols.ByteCol(m.End)},
		})
	}
	return res, nil
}

// FindLines scans whole lines for the pattern, within a time box.
func (p *pluginEditor) FindLines(id pluginapi.BufferID, q pluginapi.FindLinesQuery) (pluginapi.FindLinesResult, error) {
	d, err := p.doc(id)
	if err != nil {
		return pluginapi.FindLinesResult{Next: -1}, err
	}
	lines, next, err := search.FindLines(d.Buffer, search.LinesQuery{
		Query: search.Query{Text: q.Pattern, CaseSensitive: q.CaseSensitive, Regex: q.Regex},
		From:  q.From, To: q.To, Max: q.Max, Budget: q.Budget,
	})
	if err != nil {
		return pluginapi.FindLinesResult{Next: -1}, editorErr(err)
	}
	return pluginapi.FindLinesResult{Lines: lines, Next: next}, nil
}

// ---------- tabs ----------

// Activate makes the document the active tab.
func (p *pluginEditor) Activate(id pluginapi.BufferID) error {
	return editorErr(p.ed.ActivateID(editor.DocID(id)))
}

// Close closes the tab. Unsaved changes without force are a conflict, and no dialog opens.
// The refusal is made here, so the editor does not write it to the status message of the
// active tab, as a refused close of the user's does.
func (p *pluginEditor) Close(id pluginapi.BufferID, force bool) error {
	d, err := p.doc(id)
	if err != nil {
		return err
	}
	if !force && d.Modified() {
		return pluginapi.Errf(pluginapi.CodeConflict, "unsaved changes")
	}
	return editorErr(p.ed.CloseID(editor.DocID(id), true))
}

// MoveTab moves the tab by delta places, stopping at the ends.
func (p *pluginEditor) MoveTab(id pluginapi.BufferID, delta int) error {
	return editorErr(p.ed.MoveTab(editor.DocID(id), delta))
}

// ---------- change tracking ----------

// TrackChanges turns the change tracking of one consumer on or off.
func (p *pluginEditor) TrackChanges(key string, on bool) { p.ed.TrackChanges(key, on) }

// TakeChanges returns what the document did since the consumer's last call, in byte
// columns. The records are sequential: each is in the coordinates of the text before it.
func (p *pluginEditor) TakeChanges(id pluginapi.BufferID, consumer string) pluginapi.Changes {
	d, err := p.doc(id)
	if err != nil {
		return pluginapi.Changes{}
	}
	cs, overflow := p.ed.TakeChanges(editor.DocID(id), consumer)
	out := pluginapi.Changes{Version: versionOf(d), Overflow: overflow}
	for _, c := range cs {
		out.Edits = append(out.Edits, pluginapi.ChangeRec{
			Range: pluginapi.Range{
				From: pluginapi.Pos{Line: c.From.Line, Col: c.FromByte},
				To:   pluginapi.Pos{Line: c.To.Line, Col: c.ToByte},
			},
			Text: c.Text, Removed: c.Removed,
		})
		out.Undo = out.Undo || c.Undo
	}
	return out
}
