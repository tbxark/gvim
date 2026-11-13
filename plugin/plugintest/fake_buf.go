package plugintest

import (
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/tbxark/gvim/pluginapi"
)

// The buffer half of the fake host: reads, edits with undo, cursors, search, find_lines, tab
// operations and the async open/save/revert. Buffers are the FakeBuffer values of fakehost.go
// (AddBuffer, Buffers); everything else a buffer needs lives in bufState, kept in Ext["buf"].
//
// The fake follows the position rules which the real editor adapter
// follows too: lines and columns are 0-based, columns are byte offsets and round down to a
// rune start, a column past the end of a line clamps to the line end, a line past the last
// clamps to the end of the document (no newline added), and positions are never an error.

const bufExtKey = "buf"

type bufSnapshot struct {
	lines   []string
	carets  []pluginapi.Caret
	version pluginapi.Version
}

// bufState is the per-buffer state of the fake that FakeBuffer does not carry.
type bufState struct {
	undo, redo []bufSnapshot
	carets     []pluginapi.Caret
	saved      []string
	group      int  // UndoGroup nesting
	grouped    bool // the open group already pushed its undo entry
}

type bufPending struct {
	tok  pluginapi.Token
	kind string // "open", "save", "revert"
	id   pluginapi.BufferID
	path string
	req  pluginapi.OpenRequest
}

type bufExt struct {
	seq     uint64 // content counter: Version.Ver of the next edit
	st      map[pluginapi.BufferID]*bufState
	files   map[string]string // files Open can load
	pending []bufPending
	fail    map[string]string // "open:<path>", "save", "revert" -> "<code>: <msg>" for the completion
	chunk   int               // FindLines scans at most chunk lines per call (0: unlimited)
}

func (f *FakeHost) bx() *bufExt {
	if x, ok := f.Ext[bufExtKey].(*bufExt); ok {
		return x
	}
	x := &bufExt{st: map[pluginapi.BufferID]*bufState{}, files: map[string]string{}, fail: map[string]string{}}
	f.Ext[bufExtKey] = x
	return x
}

func (f *FakeHost) bstate(b *FakeBuffer) *bufState {
	x := f.bx()
	s := x.st[b.Info.ID]
	if s == nil {
		s = &bufState{carets: []pluginapi.Caret{{}}, saved: slices.Clone(b.Lines)}
		x.st[b.Info.ID] = s
	}
	return s
}

// SetFile makes path loadable by Open.
func (f *FakeHost) SetFile(path, text string) { f.bx().files[path] = text }

// FailBuf makes the next completion of an async buffer call fail with errText
// ("<code>: <msg>"). Keys: "open:<path>", "save", "revert".
func (f *FakeHost) FailBuf(key, errText string) { f.bx().fail[key] = errText }

// SetFindLinesChunk makes FindLines stop after n scanned lines per call, as the time box
// of the real implementation does; 0 means unlimited.
func (f *FakeHost) SetFindLinesChunk(n int) { f.bx().chunk = n }

// PendingBufferCalls is the number of async buffer calls CompleteBuffers would complete.
func (f *FakeHost) PendingBufferCalls() int { return len(f.bx().pending) }

// ---------- text helpers ----------

func bufText(b *FakeBuffer) string { return strings.Join(b.Lines, "\n") }

// bufStarts returns the byte offset of the start of each line.
func bufStarts(lines []string) []int {
	starts := make([]int, len(lines))
	off := 0
	for i, l := range lines {
		starts[i] = off
		off += len(l) + 1
	}
	return starts
}

func bufTextLen(lines []string, starts []int) int {
	if len(lines) == 0 {
		return 0
	}
	return starts[len(lines)-1] + len(lines[len(lines)-1])
}

// bufOffset clamps p and returns its byte offset in the LF-joined text.
func bufOffset(lines []string, starts []int, p pluginapi.Pos) int {
	if p.Line < 0 {
		p.Line = 0
	}
	if p.Line >= len(lines) {
		return bufTextLen(lines, starts)
	}
	l := lines[p.Line]
	col := min(max(p.Col, 0), len(l))
	for col > 0 && col < len(l) && !utf8.RuneStart(l[col]) {
		col--
	}
	return starts[p.Line] + col
}

// bufPosAt is the inverse of bufOffset for offsets inside the text.
func bufPosAt(lines []string, starts []int, off int) pluginapi.Pos {
	i := sort.Search(len(starts), func(i int) bool { return starts[i] > off }) - 1
	i = max(i, 0)
	return pluginapi.Pos{Line: i, Col: min(off-starts[i], len(lines[i]))}
}

func bufClosedErr(id pluginapi.BufferID) error {
	return pluginapi.Errf(pluginapi.CodeClosed, "buffer %d is closed", id)
}

func (f *FakeHost) writable(id pluginapi.BufferID) (*FakeBuffer, error) {
	b := f.find(id)
	switch {
	case b == nil:
		return nil, bufClosedErr(id)
	case b.Info.ReadOnly:
		return nil, pluginapi.Errf(pluginapi.CodeReadOnly, "buffer %d is read-only", id)
	case b.Info.Large:
		return nil, pluginapi.Errf(pluginapi.CodeTooLarge, "large file mode does not support this edit")
	}
	return b, nil
}

func (f *FakeHost) refreshInfo(b *FakeBuffer) {
	s := f.bstate(b)
	b.Info.LineCount = len(b.Lines)
	b.Info.Modified = !b.Info.Virtual && !slices.Equal(b.Lines, s.saved) // a virtual buffer is never modified
}

// ---------- reads ----------

// Slice returns the text of r; a reversed range is taken in order.
func (f *FakeHost) Slice(id pluginapi.BufferID, r pluginapi.Range) (string, error) {
	f.rec("Slice", id, r)
	b := f.find(id)
	if b == nil {
		return "", bufClosedErr(id)
	}
	starts := bufStarts(b.Lines)
	from, to := bufOffset(b.Lines, starts, r.From), bufOffset(b.Lines, starts, r.To)
	if from > to {
		from, to = to, from
	}
	return bufText(b)[from:to], nil
}

// ByteLength is the length of the LF-joined text.
func (f *FakeHost) ByteLength(id pluginapi.BufferID) (int, error) {
	f.rec("ByteLength", id)
	b := f.find(id)
	if b == nil {
		return 0, bufClosedErr(id)
	}
	if b.Info.Large {
		return 0, pluginapi.Errf(pluginapi.CodeTooLarge, "large file mode")
	}
	return len(bufText(b)), nil
}

// OffsetOf converts a position to a byte offset.
func (f *FakeHost) OffsetOf(id pluginapi.BufferID, p pluginapi.Pos) (int, error) {
	f.rec("OffsetOf", id, p)
	b := f.find(id)
	if b == nil {
		return 0, bufClosedErr(id)
	}
	if b.Info.Large {
		return 0, pluginapi.Errf(pluginapi.CodeTooLarge, "large file mode")
	}
	return bufOffset(b.Lines, bufStarts(b.Lines), p), nil
}

// PosOf converts a byte offset to a position; offsets are clamped to the text.
func (f *FakeHost) PosOf(id pluginapi.BufferID, offset int) (pluginapi.Pos, error) {
	f.rec("PosOf", id, offset)
	b := f.find(id)
	if b == nil {
		return pluginapi.Pos{}, bufClosedErr(id)
	}
	if b.Info.Large {
		return pluginapi.Pos{}, pluginapi.Errf(pluginapi.CodeTooLarge, "large file mode")
	}
	starts := bufStarts(b.Lines)
	return bufPosAt(b.Lines, starts, min(max(offset, 0), bufTextLen(b.Lines, starts))), nil
}

// FullText returns the LF-joined text and the version.
func (f *FakeHost) FullText(id pluginapi.BufferID) (string, pluginapi.Version, error) {
	f.rec("FullText", id)
	b := f.find(id)
	if b == nil {
		return "", pluginapi.Version{}, bufClosedErr(id)
	}
	return bufText(b), b.Info.Version, nil
}

// SavedLines returns the saved text; untitled buffers have none.
func (f *FakeHost) SavedLines(id pluginapi.BufferID) ([]string, error) {
	f.rec("SavedLines", id)
	b := f.find(id)
	if b == nil {
		return nil, bufClosedErr(id)
	}
	if b.Info.Path == "" {
		return nil, pluginapi.Errf(pluginapi.CodeNotFound, "buffer %d has no saved text", id)
	}
	return slices.Clone(f.bstate(b).saved), nil
}

// ---------- edits ----------

func (f *FakeHost) snapshot(b *FakeBuffer) bufSnapshot {
	s := f.bstate(b)
	return bufSnapshot{lines: slices.Clone(b.Lines), carets: slices.Clone(s.carets), version: b.Info.Version}
}

func (f *FakeHost) restore(b *FakeBuffer, sn bufSnapshot) {
	s := f.bstate(b)
	b.Lines = slices.Clone(sn.lines)
	s.carets = slices.Clone(sn.carets)
	b.Info.Version = sn.version
	f.refreshInfo(b)
}

// pushUndo records the state before an edit, once per undo group.
func (f *FakeHost) pushUndo(b *FakeBuffer) {
	s := f.bstate(b)
	if s.group > 0 {
		if s.grouped {
			return
		}
		s.grouped = true
	}
	s.undo = append(s.undo, f.snapshot(b))
	s.redo = nil
}

// Edit applies the edits as one undo step. Ranges are clamped, in pre-edit coordinates;
// overlapping edits are "invalid: overlapping edits". The version of the buffer takes a new
// value from the content counter.
func (f *FakeHost) Edit(id pluginapi.BufferID, edits []pluginapi.Edit, o pluginapi.EditOptions) error {
	f.rec("Edit", id, len(edits), o)
	b, err := f.writable(id)
	if err != nil {
		return err
	}
	if o.Expect != nil && *o.Expect != b.Info.Version {
		return pluginapi.Errf(pluginapi.CodeStale, "buffer %d changed (version %s, expected %s)", id, b.Info.Version, o.Expect)
	}
	if len(edits) == 0 {
		return nil
	}
	starts := bufStarts(b.Lines)
	type span struct {
		from, to int
		text     string
	}
	spans := make([]span, len(edits))
	for i, ed := range edits {
		from, to := bufOffset(b.Lines, starts, ed.Range.From), bufOffset(b.Lines, starts, ed.Range.To)
		if from > to {
			from, to = to, from
		}
		spans[i] = span{from, to, ed.Text}
	}
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].from != spans[j].from {
			return spans[i].from < spans[j].from
		}
		return spans[i].to < spans[j].to
	})
	for i := 1; i < len(spans); i++ {
		if spans[i].from < spans[i-1].to {
			return pluginapi.Errf(pluginapi.CodeInvalid, "overlapping edits")
		}
	}
	old := bufText(b)
	var sb strings.Builder
	prev := 0
	newEnds := make([]int, len(spans)) // end of each edit's inserted text in the new text
	shifts := make([]int, len(spans))  // new offset of each old edit start
	for i, sp := range spans {
		sb.WriteString(old[prev:sp.from])
		shifts[i] = sb.Len()
		sb.WriteString(sp.text)
		newEnds[i] = sb.Len()
		prev = sp.to
	}
	sb.WriteString(old[prev:])
	next := sb.String()

	// carets: map the old offsets through the sorted edits
	s := f.bstate(b)
	mapOff := func(off int) int {
		delta := 0
		for i, sp := range spans {
			switch {
			case off > sp.to || (off == sp.to && sp.from != sp.to):
				delta = newEnds[i] - sp.to
			case off >= sp.from:
				return newEnds[i] // inside, or at an insertion point: after the new text
			default:
				return off + delta
			}
		}
		return off + delta
	}
	oldLines := b.Lines
	f.pushUndo(b)
	b.Lines = strings.Split(next, "\n")
	nstarts := bufStarts(b.Lines)
	place := func(p pluginapi.Pos) pluginapi.Pos {
		off := bufOffset(oldLines, starts, p)
		if o.Cursor == pluginapi.CursorMap {
			off = mapOff(off)
		}
		return bufPosAt(b.Lines, nstarts, min(off, len(next)))
	}
	carets := make([]pluginapi.Caret, len(s.carets))
	for i, c := range s.carets {
		nc := pluginapi.Caret{Pos: place(c.Pos)}
		if c.Anchor != nil {
			a := place(*c.Anchor)
			nc.Anchor = &a
		}
		carets[i] = nc
	}
	if o.Cursor == pluginapi.CursorEnd {
		carets[0] = pluginapi.Caret{Pos: bufPosAt(b.Lines, nstarts, newEnds[len(newEnds)-1])}
	}
	s.carets = carets
	f.bx().seq++
	b.Info.Version.Ver = f.bx().seq
	f.refreshInfo(b)
	return nil
}

// UndoGroup runs fn; every edit it makes is one undo step. Groups nest.
func (f *FakeHost) UndoGroup(id pluginapi.BufferID, fn func() error) error {
	f.rec("UndoGroup", id)
	b := f.find(id)
	if b == nil {
		return bufClosedErr(id)
	}
	s := f.bstate(b)
	s.group++
	defer func() {
		s.group--
		if s.group == 0 {
			s.grouped = false
		}
	}()
	return fn()
}

// Undo restores the state before the last undo step, version included.
func (f *FakeHost) Undo(id pluginapi.BufferID) error {
	f.rec("Undo", id)
	b, err := f.writable(id)
	if err != nil {
		return err
	}
	s := f.bstate(b)
	if len(s.undo) == 0 {
		return pluginapi.Errf(pluginapi.CodeNotFound, "nothing to undo")
	}
	sn := s.undo[len(s.undo)-1]
	s.undo = s.undo[:len(s.undo)-1]
	s.redo = append(s.redo, f.snapshot(b))
	f.restore(b, sn)
	return nil
}

// Redo is the inverse of Undo.
func (f *FakeHost) Redo(id pluginapi.BufferID) error {
	f.rec("Redo", id)
	b, err := f.writable(id)
	if err != nil {
		return err
	}
	s := f.bstate(b)
	if len(s.redo) == 0 {
		return pluginapi.Errf(pluginapi.CodeNotFound, "nothing to redo")
	}
	sn := s.redo[len(s.redo)-1]
	s.redo = s.redo[:len(s.redo)-1]
	s.undo = append(s.undo, f.snapshot(b))
	f.restore(b, sn)
	return nil
}

// ---------- cursors ----------

func (f *FakeHost) clampPos(b *FakeBuffer, p pluginapi.Pos) pluginapi.Pos {
	starts := bufStarts(b.Lines)
	return bufPosAt(b.Lines, starts, bufOffset(b.Lines, starts, p))
}

// Cursors returns the carets, primary first.
func (f *FakeHost) Cursors(id pluginapi.BufferID) ([]pluginapi.Caret, error) {
	f.rec("Cursors", id)
	b := f.find(id)
	if b == nil {
		return nil, bufClosedErr(id)
	}
	return slices.Clone(f.bstate(b).carets), nil
}

// SetCursor moves the primary caret; extend keeps (or starts) a selection.
func (f *FakeHost) SetCursor(id pluginapi.BufferID, p pluginapi.Pos, extend, reveal, center bool) error {
	f.rec("SetCursor", id, p, extend, reveal, center)
	if f.SetCursorErr != nil {
		return f.SetCursorErr
	}
	b := f.find(id)
	if b == nil {
		return bufClosedErr(id)
	}
	s := f.bstate(b)
	old := s.carets[0]
	c := pluginapi.Caret{Pos: f.clampPos(b, p)}
	if extend {
		a := old.Pos
		if old.Anchor != nil {
			a = *old.Anchor
		}
		c.Anchor = &a
	}
	s.carets = []pluginapi.Caret{c}
	return nil
}

// SetSelection selects r with the caret at its end.
func (f *FakeHost) SetSelection(id pluginapi.BufferID, r pluginapi.Range) error {
	f.rec("SetSelection", id, r)
	b := f.find(id)
	if b == nil {
		return bufClosedErr(id)
	}
	a := f.clampPos(b, r.From)
	f.bstate(b).carets = []pluginapi.Caret{{Pos: f.clampPos(b, r.To), Anchor: &a}}
	return nil
}

// ---------- search ----------

func bufMatcher(text string, regex, caseSensitive, wholeWord bool) (*regexp.Regexp, error) {
	if !regex {
		text = regexp.QuoteMeta(text)
	}
	if wholeWord {
		text = `\b(?:` + text + `)\b`
	}
	if !caseSensitive {
		text = `(?i)` + text
	}
	re, err := regexp.Compile(text)
	if err != nil {
		return nil, pluginapi.Errf(pluginapi.CodeInvalid, "bad pattern: %v", err)
	}
	return re, nil
}

// Search finds up to Max matches inside Within (default: the whole buffer). Truncated is
// true when more matches exist than were returned.
func (f *FakeHost) Search(id pluginapi.BufferID, q pluginapi.SearchQuery) (pluginapi.SearchResult, error) {
	f.rec("Search", id, q)
	b := f.find(id)
	if b == nil {
		return pluginapi.SearchResult{}, bufClosedErr(id)
	}
	if b.Info.Large {
		return pluginapi.SearchResult{}, pluginapi.Errf(pluginapi.CodeTooLarge, "large file mode")
	}
	re, err := bufMatcher(q.Text, q.Regex, q.CaseSensitive, q.WholeWord)
	if err != nil {
		return pluginapi.SearchResult{}, err
	}
	limit := q.Max
	if limit <= 0 {
		limit = 1000
	}
	starts := bufStarts(b.Lines)
	text := bufText(b)
	lo, hi := 0, len(text)
	if q.Within != nil {
		lo, hi = bufOffset(b.Lines, starts, q.Within.From), bufOffset(b.Lines, starts, q.Within.To)
		if lo > hi {
			lo, hi = hi, lo
		}
	}
	var res pluginapi.SearchResult
	for _, m := range re.FindAllStringIndex(text[lo:hi], limit+1) {
		if m[1] == m[0] {
			continue
		}
		if len(res.Ranges) == limit {
			res.Truncated = true
			break
		}
		res.Ranges = append(res.Ranges, pluginapi.Range{
			From: bufPosAt(b.Lines, starts, lo+m[0]), To: bufPosAt(b.Lines, starts, lo+m[1]),
		})
	}
	return res, nil
}

// FindLines scans whole lines in [From, To). With SetFindLinesChunk it stops after that
// many lines and reports where it stopped in Next.
func (f *FakeHost) FindLines(id pluginapi.BufferID, q pluginapi.FindLinesQuery) (pluginapi.FindLinesResult, error) {
	f.rec("FindLines", id, q)
	b := f.find(id)
	if b == nil {
		return pluginapi.FindLinesResult{Next: -1}, bufClosedErr(id)
	}
	re, err := bufMatcher(q.Pattern, q.Regex, q.CaseSensitive, false)
	if err != nil {
		return pluginapi.FindLinesResult{Next: -1}, err
	}
	limit := q.Max
	if limit <= 0 {
		limit = 10000
	}
	from := max(q.From, 0)
	end := len(b.Lines)
	if q.To > 0 {
		end = min(q.To, end)
	}
	chunk := f.bx().chunk
	res := pluginapi.FindLinesResult{Next: -1}
	scanned := 0
	for i := from; i < end; i++ {
		if chunk > 0 && scanned >= chunk {
			res.Next = i
			return res, nil
		}
		scanned++
		if re.MatchString(b.Lines[i]) {
			res.Lines = append(res.Lines, i)
			if len(res.Lines) >= limit && i+1 < end {
				res.Next = i + 1
				return res, nil
			}
		}
	}
	return res, nil
}

// ---------- tabs and state ----------

// SetLanguage sets the language name.
func (f *FakeHost) SetLanguage(id pluginapi.BufferID, lang string) error {
	f.rec("SetLanguage", id, lang)
	b := f.find(id)
	if b == nil {
		return bufClosedErr(id)
	}
	b.Info.Language = lang
	return nil
}

// Activate makes the buffer the active tab.
func (f *FakeHost) Activate(id pluginapi.BufferID) error {
	f.rec("Activate", id)
	if f.find(id) == nil {
		return bufClosedErr(id)
	}
	f.SetActive(id)
	return nil
}

// Close closes the buffer; unsaved changes without force are a conflict.
func (f *FakeHost) Close(id pluginapi.BufferID, force bool) error {
	f.rec("Close", id, force)
	b := f.find(id)
	if b == nil {
		return bufClosedErr(id)
	}
	if b.Info.Modified && !force {
		return pluginapi.Errf(pluginapi.CodeConflict, "unsaved changes")
	}
	f.RemoveBuffer(id)
	delete(f.bx().st, id)
	return nil
}

// MoveTab moves the tab by delta places, clamped to the ends.
func (f *FakeHost) MoveTab(id pluginapi.BufferID, delta int) error {
	f.rec("MoveTab", id, delta)
	for i, b := range f.Buffers {
		if b.Info.ID != id {
			continue
		}
		j := min(max(i+delta, 0), len(f.Buffers)-1)
		f.Buffers = slices.Delete(f.Buffers, i, i+1)
		f.Buffers = slices.Insert(f.Buffers, j, b)
		return nil
	}
	return bufClosedErr(id)
}

// ---------- async: open, save, revert ----------

// Open records the request; CompleteBuffers completes it.
func (f *FakeHost) Open(t pluginapi.Token, r pluginapi.OpenRequest) error {
	f.rec("Open", t, r)
	if r.Path == "" {
		return pluginapi.Errf(pluginapi.CodeInvalid, "empty path")
	}
	x := f.bx()
	x.pending = append(x.pending, bufPending{tok: t, kind: "open", path: r.Path, req: r})
	return nil
}

// Save records the request; CompleteBuffers completes it.
func (f *FakeHost) Save(t pluginapi.Token, id pluginapi.BufferID, path string) error {
	f.rec("Save", t, id, path)
	b := f.find(id)
	if b == nil {
		return bufClosedErr(id)
	}
	if b.Info.Virtual {
		return pluginapi.Errf(pluginapi.CodeInvalid, "a virtual buffer cannot be saved")
	}
	x := f.bx()
	x.pending = append(x.pending, bufPending{tok: t, kind: "save", id: id, path: path})
	return nil
}

// Revert records the request; CompleteBuffers completes it.
func (f *FakeHost) Revert(t pluginapi.Token, id pluginapi.BufferID) error {
	f.rec("Revert", t, id)
	b := f.find(id)
	if b == nil {
		return bufClosedErr(id)
	}
	if b.Info.Path == "" {
		return pluginapi.Errf(pluginapi.CodeInvalid, "buffer %d has no file to revert to", id)
	}
	x := f.bx()
	x.pending = append(x.pending, bufPending{tok: t, kind: "revert", id: id})
	return nil
}

// completeBuffers performs the pending async calls and returns their completions.
func (f *FakeHost) completeBuffers() []pluginapi.Completion {
	x := f.bx()
	pending := x.pending
	x.pending = nil
	var out []pluginapi.Completion
	for _, p := range pending {
		c := pluginapi.Completion{Token: p.tok}
		key := p.kind
		if p.kind == "open" {
			key = "open:" + p.path
		}
		if msg, ok := x.fail[key]; ok {
			delete(x.fail, key)
			c.Err = msg
			out = append(out, c)
			continue
		}
		switch p.kind {
		case "open":
			id := f.FindBuffer(p.path)
			if id == 0 {
				text, ok := x.files[p.path]
				if !ok {
					c.Err = "not_found: " + p.path
					break
				}
				id = f.AddBuffer(p.path, text)
			}
			if p.req.Focus {
				f.SetActive(id)
			}
			if p.req.Pos != nil {
				_ = f.SetCursor(id, *p.req.Pos, false, true, false)
			}
			c.Values = []any{id}
		case "save":
			b := f.find(p.id)
			switch {
			case b == nil:
				c.Err = "closed: buffer is closed"
			case p.path == "" && b.Info.Path == "":
				c.Err = "invalid: the buffer has no path"
			default:
				if p.path != "" {
					b.Info.Path = p.path
					b.Info.Name = p.path[strings.LastIndexByte(p.path, '/')+1:]
				}
				f.bstate(b).saved = slices.Clone(b.Lines)
				f.refreshInfo(b)
				c.Values = []any{true}
			}
		case "revert":
			b := f.find(p.id)
			if b == nil {
				c.Err = "closed: buffer is closed"
				break
			}
			s := f.bstate(b)
			b.Lines = slices.Clone(s.saved)
			s.undo, s.redo = nil, nil
			s.carets = []pluginapi.Caret{{}}
			// A new Buffer object: the generation changes and the content counter restarts.
			b.Info.Version = pluginapi.Version{Gen: b.Info.Version.Gen + 1}
			f.refreshInfo(b)
			c.Values = []any{true}
		}
		out = append(out, c)
	}
	return out
}

// CompleteBuffers completes the pending Open, Save and Revert calls of the fake host the way
// the ui would after the file work: it resolves each token and settles.
func (h *Harness) CompleteBuffers() {
	h.T.Helper()
	for _, c := range h.Host.completeBuffers() {
		h.Resolve(c)
	}
}
