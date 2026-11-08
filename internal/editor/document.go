package editor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/encoding"
)

// Document is one open file: its text, the cursor, an optional selection, and
// the first visible line of its viewport.
type Document struct {
	// Path is the absolute path of the file, or "" for an unnamed buffer.
	Path   string
	Buffer *buffer.Buffer
	Cursor buffer.Pos
	// Sel is nil when nothing is selected. Its Head always equals Cursor.
	Sel *buffer.Selection
	// extra holds the carets other than the primary one (Cursor and Sel), in
	// document order. It is nil when there is one caret. See Cursors.
	extra []Caret
	// block is the block selection in progress, or nil. See blockStep.
	block *blockState
	// Top is the index of the first line shown in the editor pane.
	Top int
	// seen is the caret as ensureVisible last saw it. The scroll-off margin
	// applies only when the caret has moved since.
	seen buffer.Pos
	// preview marks a tab opened by OpenPreview. A preview tab is replaced by
	// the next preview and becomes permanent once it is edited or opened again
	// with OpenFile.
	preview bool

	// foldMemo is the fold ranges found last, see Folds.
	foldMemo foldMemo
	// changesMemo is the change marks computed last, see MemoChanges.
	changesMemo changesMemo
	// valueMemo holds what MemoValue kept for the current text.
	valueMemo valueMemo
	// longMemo is the answer HasLongLine gave last.
	longMemo longMemo

	// saved is the text as it was at open or the last save. It is the only
	// saved-state record: SavedLines and LinesChangedSinceSave read it.
	saved string
	// savedGen changes whenever saved is set. See setSaved.
	savedGen uint64
	// savedSplit is saved split into lines, kept for the savedGen it was split at, so
	// that the change marks of a big document do not split the text on every edit.
	savedSplit splitMemo
	// encoding is the label of the encoding the saved text is written in (see
	// encodingLabel). It is computed on open, on save and on set_encoding so
	// that rendering never scans the text.
	encoding string
	// eol is the line ending of the saved text, eolLF or eolCRLF. The buffer
	// always holds "\n" as its line break, and fileText converts it back.
	eol string
	// ends is the ending of each line break of saved when the file mixes LF and CRLF,
	// and nil otherwise. A save writes the recorded ending for a line it did not
	// change. See files_lineend.go.
	ends []string
	// disk is the state of the file when it was opened or last saved. A save
	// compares it with the file on disk, so an outside change is not overwritten.
	disk diskState

	// want is the column that vertical moves try to keep. -1 means unset,
	// so the next vertical move takes the current column.
	want int

	// untitled is N for a buffer created by New as Untitled-N. It is 0 for the
	// placeholder buffer and for any buffer that has a path.
	untitled int
	// language is the lexer name the UI set with SetLanguage. It is cleared
	// when the path changes, because the lexer depends on the file name.
	language string
	// revertAuto is set while ApplyRevert applies an automatic reload (RevertPlan.Auto), so
	// that the status it leaves says "Reloaded".
	revertAuto bool
	// status is the message shown while this tab is active. Each tab keeps its
	// own, so switching tabs never shows another tab's message.
	status string
	// statusErr is the status text when the status is an error. It equals status only
	// while the message is that error, so a later message is not taken for one.
	statusErr string
	// saveNote is what the status says after the last save of a document that is not large,
	// in place of "Saved", or empty. See savedStatus.
	saveNote string
	// codec is the encoding name the text was read with, from internal/encoding.
	// Save encodes with it. "" means utf-8, the encoding of a new document.
	codec string
	// readOnly refuses every change to the text and to the file. See readonly.go.
	readOnly bool
	// binary marks a file that is not text. It is read-only and cannot be saved. See
	// binary.go.
	binary bool
	// metaDirty is set when the encoding or the line ending was changed since the
	// last save, which leaves the text as it is but changes the file. See
	// markMetaChanged.
	metaDirty bool
	// findRange limits the find actions to this range when it is set. See
	// doc_search.go.
	findRange *buffer.Selection
	// folded holds the start lines of the folds that are collapsed. See fold.go.
	folded map[int]bool
	// lastMulti is the last multi-caret edit, for undo and redo. See multiEditRecord.
	lastMulti *multiEditRecord
	// view holds the display settings of the document. See viewsettings.go.
	view ViewSettings
	// large is the state of a document in large file mode, and nil for any other.
	// See large.go.
	large *largeDoc
	// planErr is why the edit being planned must not be made, or nil. See failPlan.
	planErr error
	// seam is the id, buffer generation and change tracking of the document. See docid.go.
	seam docSeam
	// ext is the plugin-facing state: virtual documents, buffer mode, text properties.
	// See virtual.go.
	ext docExt
}

// Line endings a document can save with.
const (
	eolLF   = "LF"
	eolCRLF = "CRLF"
)

// newDocument returns a Document holding text at path. An empty path means
// an unnamed buffer. Its line ending is LF until the caller sets another.
func newDocument(path, text string) *Document {
	return &Document{Path: path, Buffer: buffer.New(text), saved: text, encoding: encodingOf(path, text), want: -1, eol: eolLF}
}

// splitLineEnding turns the CRLF breaks of data into LF, the form the buffer
// holds, and reports the ending the file uses. CRLF wins only when its breaks
// outnumber the bare LF breaks, so a tie, or a text with no break, is LF. The
// ending of each break of a mixed file is kept by lineEnds.
func splitLineEnding(data string) (text, eol string) {
	crlf := strings.Count(data, "\r\n")
	lf := strings.Count(data, "\n") - crlf
	eol = eolLF
	if crlf > lf {
		eol = eolCRLF
	}
	return strings.ReplaceAll(data, "\r\n", "\n"), eol
}

// setSaved records text as the saved state. Every assignment of d.saved goes through
// it, so that what is cached against the saved text (MemoChanges) is dropped.
func (d *Document) setSaved(text string) {
	d.saved = text
	d.savedGen = genCounter.Add(1)
}

// MemoChanges returns what compute returns, and calls it again only when the text, or the
// saved text it is compared with, is not the one of the last call. The pane asks for the
// change marks on every repaint; they cost a pass over all lines. The maps are shared
// between calls, so the caller must not change them.
func (d *Document) MemoChanges(compute func() (bar, scroll map[int]bool)) (bar, scroll map[int]bool) {
	key := changesKey{gen: d.Gen(), ver: d.Buffer.Version(), saved: d.savedGen}
	if !d.changesMemo.ok || d.changesMemo.key != key {
		d.changesMemo.bar, d.changesMemo.scroll = compute()
		d.changesMemo.key, d.changesMemo.ok = key, true
	}
	return d.changesMemo.bar, d.changesMemo.scroll
}

// MemoValue returns the value kept for key, or calls compute and keeps what it returns.
// The values are dropped when the text changes, so a caller keeps what it worked out
// from the text, such as the layout of a long line, across the repaints and the queries
// of a single edit. At most memoValues are kept.
func (d *Document) MemoValue(key any, compute func() any) any {
	m := &d.valueMemo
	if ver := (changesKey{gen: d.Gen(), ver: d.Buffer.Version()}); m.vals == nil || m.key != ver || len(m.vals) >= memoValues {
		m.key, m.vals = ver, make(map[any]any)
	}
	v, ok := m.vals[key]
	if !ok {
		v = compute()
		m.vals[key] = v
	}
	return v
}

// HasLongLine reports whether any line of the document has more than limit runes. A large
// document answers false: its lines are read through windows already. The answer is kept
// until the text changes, so the pane may ask on every frame.
func (d *Document) HasLongLine(limit int) bool {
	if d.large != nil {
		return false
	}
	m := &d.longMemo
	key := changesKey{gen: d.Gen(), ver: d.Buffer.Version()}
	if !m.ok || m.key != key || m.limit != limit {
		m.has = false
		for i := range d.Buffer.Lines() {
			if d.Buffer.LineLen(i) > limit {
				m.has = true
				break
			}
		}
		m.key, m.limit, m.ok = key, limit, true
	}
	return m.has
}

// longMemo is the answer of HasLongLine for the text of version key.
type longMemo struct {
	ok, has bool
	key     changesKey
	limit   int
}

// memoValues is how many values MemoValue keeps for one text: a screenful and some.
const memoValues = 256

// valueMemo is the values MemoValue keeps for the text of version key.
type valueMemo struct {
	key  changesKey
	vals map[any]any
}

// splitMemo is the saved text split into lines.
type splitMemo struct {
	ok    bool
	gen   uint64
	lines []string
}

type changesKey struct{ gen, ver, saved uint64 }

// changesMemo is the change marks MemoChanges computed last.
type changesMemo struct {
	ok          bool
	key         changesKey
	bar, scroll map[int]bool
}

// SavedLines returns the saved text split into lines on '\n', the same way
// buffer.New splits its content. The text is the one at open or at the last
// save. The result is a new slice on every call, so the caller may change it.
// An unnamed, unsaved buffer has the single empty line [""]. A large document does
// not keep its saved text, so it returns nil.
func (d *Document) SavedLines() []string {
	return slices.Clone(d.SavedLinesShared())
}

// SavedLinesShared is SavedLines without the copy: the slice is kept by the document, so
// the caller must not change it, and it is only good until the saved text changes. The pane
// reads it on every frame, and a copy of a big document's lines is a megabyte.
func (d *Document) SavedLinesShared() []string {
	if d.large != nil {
		return nil
	}
	if m := &d.savedSplit; !m.ok || m.gen != d.savedGen {
		m.lines, m.gen, m.ok = strings.Split(d.saved, "\n"), d.savedGen, true
	}
	return d.savedSplit.lines
}

// EditedLines returns the lines [lo, hi) that may differ from the saved text: the lines
// before lo and from hi on are the saved ones, counted from the start and from the end of
// the text. ok is false for a large document, which holds no saved text. See
// buffer.Buffer.EditedLines.
func (d *Document) EditedLines() (lo, hi int, ok bool) {
	if d.large != nil {
		return 0, 0, false
	}
	return d.Buffer.EditedLines()
}

// LinesChangedSinceSave returns the 0-based indexes of lines whose content
// differs from SavedLines: the text at open or at the last save. The
// comparison is line by line, so inserting or deleting a line marks every
// line below it as changed. It returns nil when nothing differs, and for a large
// document, which has no saved text to compare with.
func (d *Document) LinesChangedSinceSave() []int {
	if d.large != nil {
		return nil
	}
	saved := d.SavedLines()
	var changed []int
	for i := range d.Buffer.Lines() {
		if i >= len(saved) || d.Buffer.Line(i) != saved[i] {
			changed = append(changed, i)
		}
	}
	return changed
}

// Name returns the file's base name. A buffer with no path is named Untitled-N
// when New created it, and "[No Name]" for the placeholder buffer.
func (d *Document) Name() string {
	switch {
	case d.Path != "":
		return filepath.Base(d.Path)
	case d.ext.virtual:
		return "*" + d.ext.name + "*"
	case d.untitled > 0:
		return fmt.Sprintf("Untitled-%d", d.untitled)
	default:
		return "[No Name]"
	}
}

// Untitled reports whether the buffer has no file name. Save returns ErrNoPath
// for it, and the caller should run Save As instead.
func (d *Document) Untitled() bool {
	return d.Path == ""
}

// Language returns the lexer name set with SetLanguage, or "" when none is set.
func (d *Document) Language() string {
	return d.language
}

// Modified reports whether the document has unsaved changes: edits to the text, a new
// encoding or line ending, or stashed edits of a large document that wait for the line
// index or could not be put back. The stash is the user's text, so closing the tab asks
// first, and a quit does.
func (d *Document) Modified() bool {
	return d.dirty() || d.holdsStash()
}

// dirty reports whether saving d would write something: edits to the text, or a new
// encoding or line ending. A stash that is held does not count, since the text on
// screen is not it.
func (d *Document) dirty() bool {
	return !d.ext.virtual && (d.Buffer.Modified() || d.metaDirty)
}

// HasSelection reports whether a non-empty selection exists.
func (d *Document) HasSelection() bool {
	return d.Sel != nil && d.Sel.Anchor != d.Sel.Head
}

// ErrNoPath is returned by Save when the active buffer has no file name. The
// caller runs Save As, which names the buffer and writes it.
var ErrNoPath = errors.New("buffer has no file name")

// ErrChangedOnDisk is returned by Save when the file was changed on disk after
// it was opened or last saved. Save writes nothing. Save As, to the same path,
// writes the buffer over the file.
var ErrChangedOnDisk = errors.New("changed on disk since it was opened; not saved. Save As keeps your version")

// readDocument loads path, decoding it with decodeFile. A missing file yields an
// empty document that will create the file on save, as an editor does for a new
// path. That document takes the spec's line ending, the ending for a new file. A file
// of at least the spec's large threshold opens in large file mode instead, without
// being read whole (see readLargeDocument).
func readDocument(path string, spec loadSpec) (*Document, error) {
	// The state is taken before the read. A change between the two then shows
	// as a difference on save, which is the safe side.
	disk := diskStateOf(path)
	if disk.special {
		return nil, fmt.Errorf("open %s: not a regular file", path)
	}
	if disk.exists && spec.large.isLarge(disk.size) {
		return readLargeDocument(path, disk, spec.large)
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		d := newDocument(path, "")
		d.eol = spec.eol
		return d, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if encoding.IsBinary(data) {
		return binaryDocument(path, data, disk), nil
	}
	decoded, codec, err := decodeFile(data)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	text, eol := splitLineEnding(decoded)
	d := newDocument(path, text)
	d.eol = eol
	d.ends = lineEnds(decoded)
	d.disk = disk
	d.codec = codec
	d.encoding = encodingLabel(codec, text)
	return d, nil
}

// fileText returns text as it is written to disk: each "\n" becomes the
// document's line ending.
func (d *Document) fileText(text string) string {
	if d.ends != nil {
		return d.mixedText(text)
	}
	if d.eol == eolCRLF {
		return strings.ReplaceAll(text, "\n", "\r\n")
	}
	return text
}

// tidyForSave applies the save-time options to the buffer, so the text written
// is the text the buffer holds afterwards. The edits form one undo step. The
// caret and selection are kept inside the text they were on. A large document is
// tidied only when its index is complete, because the last line is not known before,
// and only with the final newline: trimming would edit every line of the file. A
// refused edit leaves the document as it is: the file is saved without that change,
// and the status after the save says so (see Document.savedStatus).
func (d *Document) tidyForSave(o Options) {
	if !o.TrimTrailingWhitespace && !o.EnsureFinalNewline {
		return
	}
	if d.large != nil && !d.Index().Complete {
		return
	}
	before := d.contentMark()
	d.Buffer.Group(func() {
		if o.TrimTrailingWhitespace && d.large == nil {
			d.trimTrailingWhitespace()
		}
		if o.EnsureFinalNewline {
			d.ensureFinalNewline()
		}
	})
	if d.large != nil && d.Buffer.Refusal() != nil {
		d.large.tidyNote = "Saved without final newline: last line not editable"
	}
	if d.contentMark() != before {
		// The edit is the newest undo group now, so a multi-caret record is stale.
		d.lastMulti = nil
	}
	if caret := d.Buffer.Clamp(d.Cursor); caret != d.Cursor {
		d.Cursor = caret
		d.want = -1
	}
	if d.Sel != nil {
		d.Sel.Anchor = d.Buffer.Clamp(d.Sel.Anchor)
		d.Sel.Head = d.Cursor
		if d.Sel.Anchor == d.Sel.Head {
			d.Sel = nil
		}
	}
}

// trimTrailingWhitespace removes the trailing spaces and tabs of every line.
func (d *Document) trimTrailingWhitespace() {
	for i := range d.Buffer.Lines() {
		line := d.Buffer.Line(i)
		trimmed := strings.TrimRight(line, " \t")
		if trimmed == line {
			continue
		}
		from := buffer.Pos{Line: i, Col: utf8.RuneCountInString(trimmed)}
		d.Buffer.Delete(from, buffer.Pos{Line: i, Col: d.Buffer.LineLen(i)})
	}
}

// ensureFinalNewline ends the text with a line break. An empty text stays
// empty, and so does one whose last line is already empty.
func (d *Document) ensureFinalNewline() {
	last := d.Buffer.Lines() - 1
	if d.Buffer.LineLen(last) == 0 {
		return
	}
	d.Buffer.Insert(d.Buffer.End(), "\n")
}

// move places the cursor at p. With extend set, the selection is grown from
// its existing anchor, or started at the old cursor. Otherwise it is cleared.
// A caret move ends the current undo group, so text typed afterwards starts a
// new step even inside the same wall-clock bucket.
func (d *Document) move(p buffer.Pos, extend bool) {
	d.Buffer.Break()
	if !extend {
		d.Cursor = p
		d.Sel = nil
		return
	}
	if d.Sel == nil {
		d.Sel = &buffer.Selection{Anchor: d.Cursor}
	}
	d.Cursor = p
	d.Sel.Head = p
}

// deleteSelection removes the selected text and leaves the cursor where it
// began. It reports whether anything was removed.
func (d *Document) deleteSelection() bool {
	if !d.HasSelection() {
		return false
	}
	from, to := d.Sel.Ordered()
	d.Buffer.Delete(from, to)
	d.Cursor = from
	d.Sel = nil
	return true
}

// Encoding reports the encoding of the saved text, such as "UTF-8", "ASCII" (utf-8
// text with no byte above 0x7f), "Windows-1252" or "UTF-16 LE". See encodingLabel.
//
// The utf-8 labels of a document with a path follow its text as it is edited, because
// the buffer counts its non-ASCII runes. A large document keeps the label it had at
// open and save, and can only go from ASCII to UTF-8 as text is typed into it. An
// untitled document keeps "UTF-8".
func (d *Document) Encoding() string {
	if d.codecName() != defaultCodec || d.Path == "" {
		return d.encoding
	}
	if d.large != nil {
		if d.encoding == "ASCII" && d.Buffer.NonASCII() {
			return "UTF-8"
		}
		return d.encoding
	}
	if d.Buffer.NonASCII() {
		return "UTF-8"
	}
	return "ASCII"
}

// LineEnding is "LF" or "CRLF", the ending of the saved text. It is detected
// when the file is opened, and a new document takes Options.DefaultLineEnding.
func (d *Document) LineEnding() string { return d.eol }

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
	d.markMetaChanged(eol != d.eol || d.ends != nil)
	d.eol = eol
	// An explicit choice writes one ending for every line.
	d.ends = nil
	e.setStatus("Line ending " + d.eol)
	return nil
}

// encodingOf is the encoding label for text saved at path.
func encodingOf(path, text string) string {
	if path == "" {
		return "UTF-8"
	}
	for i := 0; i < len(text); i++ {
		if text[i] >= 0x80 {
			return "UTF-8"
		}
	}
	return "ASCII"
}
