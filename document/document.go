package document

import (
	"errors"
	"fmt"
	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/encoding"
	"github.com/tbxark/gvim/fold"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
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
	Extra []Caret
	// block is the block selection in progress, or nil. See blockStep.
	Block *blockState
	// Top is the index of the first line shown in the editor pane.
	Top int
	// seen is the caret as ensureVisible last saw it. The scroll-off margin
	// applies only when the caret has moved since.
	Seen buffer.Pos
	// preview marks a tab opened by OpenPreview. A preview tab is replaced by
	// the next preview and becomes permanent once it is edited or opened again
	// with OpenFile.
	preview bool

	// foldMemo is the fold ranges found last, see Folds.
	FoldMemo foldMemo
	// externalFolds are the fold ranges an external source (a language server)
	// gave for the current text, ordered by start line, and externalRev counts the
	// sets. Folds merges them with the indentation folds; the revision invalidates
	// the fold memo. See SetExternalFolds.
	externalFolds []fold.Range
	externalRev   uint64
	// changesMemo is the change marks computed last, see MemoChanges.
	ChangesMemo changesMemo
	// valueMemo holds what MemoValue kept for the current text.
	ValueMemo valueMemo
	// longMemo is the answer HasLongLine gave last.
	LongMemo longMemo

	// saved is the text as it was at open or the last save. It is the only
	// saved-state record: SavedLines and LinesChangedSinceSave read it.
	Saved string
	// savedGen changes whenever saved is set. See setSaved.
	SavedGen uint64
	// savedSplit is saved split into lines, kept for the savedGen it was split at, so
	// that the change marks of a big document do not split the text on every edit.
	SavedSplit splitMemo
	// encoding is the label of the encoding the saved text is written in (see
	// encodingLabel). It is computed on open, on save and on set_encoding so
	// that rendering never scans the text.
	encoding string
	// eol is the line ending of the saved text, eolLF or eolCRLF. The buffer
	// always holds "\n" as its line break, and fileText converts it back.
	Eol string
	// ends is the ending of each line break of saved when the file mixes LF and CRLF,
	// and nil otherwise. A save writes the recorded ending for a line it did not
	// change. See files_lineend.go.
	Ends []string
	// disk is the state of the file when it was opened or last saved. A save
	// compares it with the file on disk, so an outside change is not overwritten.
	Disk diskState

	// want is the column that vertical moves try to keep. -1 means unset,
	// so the next vertical move takes the current column.
	Want int
	// virtualLines is how many empty rows below the last line the caret sits on
	// (vertical virtual space, editor.virtual_space "on"). It is meaningful only
	// while the caret sits at the buffer end; see virtualspace.go.
	virtualLines int
	// virtual is how many display columns the caret sits past the end of its line
	// (the virtual-space feature, editor.virtual_space). The caret's byte position
	// stays on the text; this is view state that typing materializes as spaces. See
	// virtualspace.go.
	virtual int

	// untitled is N for a buffer created by New as Untitled-N. It is 0 for the
	// placeholder buffer and for any buffer that has a path.
	untitled int
	// language is the lexer name the UI set with SetLanguage. It is cleared
	// when the path changes, because the lexer depends on the file name.
	language string
	// revertAuto is set while ApplyRevert applies an automatic reload (RevertPlan.Auto), so
	// that the status it leaves says "Reloaded".
	RevertAuto bool
	// status is the message shown while this tab is active. Each tab keeps its
	// own, so switching tabs never shows another tab's message.
	Status string
	// statusErr is the status text when the status is an error. It equals status only
	// while the message is that error, so a later message is not taken for one.
	StatusErr string
	// saveNote is what the status says after the last save of a document that is not large,
	// in place of "Saved", or empty. See savedStatus.
	SaveNote string
	// codec is the encoding name the text was read with, from encoding.
	// Save encodes with it. "" means utf-8, the encoding of a new document.
	Codec string
	// readOnly refuses every change to the text and to the file. See readonly.go.
	readOnly bool
	// binary marks a file that is not text. It is read-only and cannot be saved. See
	// binary.go.
	binary bool
	// metaDirty is set when the encoding or the line ending was changed since the
	// last save, which leaves the text as it is but changes the file. See
	// markMetaChanged.
	MetaDirty bool
	// findRange limits the find actions to this range when it is set. See
	// doc_search.go.
	FindRange *buffer.Selection
	// folded holds the start lines of the folds that are collapsed. See fold.go.
	folded map[int]bool
	// lastMulti is the last multi-caret edit, for undo and redo. See multiEditRecord.
	LastMulti *multiEditRecord
	// view holds the display settings of the document. See viewsettings.go.
	view ViewSettings
	// large is the state of a document in large file mode, and nil for any other.
	// See large.go.
	large *largeDoc
	// planErr is why the edit being planned must not be made, or nil. See failPlan.
	PlanErr error
	// seam is the id, buffer generation and change tracking of the document. See docid.go.
	seam docSeam
	// ext is the plugin-facing state: virtual documents, buffer mode, text properties.
	// See virtual.go.
	ext docExt
	// settings are the editor options this document reads. See Settings.
	Settings Settings
}

// Line endings a document can save with.
const (
	eolLF   = "LF"
	eolCRLF = "CRLF"
)

// newDocument returns a Document holding text at path. An empty path means
// an unnamed buffer. Its line ending is LF until the caller sets another.
func newDocument(path, text string) *Document {
	return &Document{Path: path, Buffer: buffer.New(text), Saved: text, encoding: encodingOf(path, text), Want: -1, Eol: eolLF}
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
func (d *Document) SetSaved(text string) {
	d.Saved = text
	d.SavedGen = genCounter.Add(1)
}

// MemoChanges returns what compute returns, and calls it again only when the text, or the
// saved text it is compared with, is not the one of the last call. The pane asks for the
// change marks on every repaint; they cost a pass over all lines. The maps are shared
// between calls, so the caller must not change them.
func (d *Document) MemoChanges(compute func() (bar, scroll map[int]bool)) (bar, scroll map[int]bool) {
	key := changesKey{Gen: d.Gen(), Ver: d.Buffer.Version(), Saved: d.SavedGen}
	if !d.ChangesMemo.Ok || d.ChangesMemo.Key != key {
		d.ChangesMemo.Bar, d.ChangesMemo.Scroll = compute()
		d.ChangesMemo.Key, d.ChangesMemo.Ok = key, true
	}
	return d.ChangesMemo.Bar, d.ChangesMemo.Scroll
}

// MemoValue returns the value kept for key, or calls compute and keeps what it returns.
// The values are dropped when the text changes, so a caller keeps what it worked out
// from the text, such as the layout of a long line, across the repaints and the queries
// of a single edit. At most memoValues are kept.
func (d *Document) MemoValue(key any, compute func() any) any {
	m := &d.ValueMemo
	if ver := (changesKey{Gen: d.Gen(), Ver: d.Buffer.Version()}); m.Vals == nil || m.Key != ver || len(m.Vals) >= memoValues {
		m.Key, m.Vals = ver, make(map[any]any)
	}
	v, ok := m.Vals[key]
	if !ok {
		v = compute()
		m.Vals[key] = v
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
	m := &d.LongMemo
	key := changesKey{Gen: d.Gen(), Ver: d.Buffer.Version()}
	if !m.Ok || m.Key != key || m.Limit != limit {
		m.Has = false
		for i := range d.Buffer.Lines() {
			if d.Buffer.LineLen(i) > limit {
				m.Has = true
				break
			}
		}
		m.Key, m.Limit, m.Ok = key, limit, true
	}
	return m.Has
}

// longMemo is the answer of HasLongLine for the text of version key.
type longMemo struct {
	Ok, Has bool
	Key     changesKey
	Limit   int
}

// memoValues is how many values MemoValue keeps for one text: a screenful and some.
const memoValues = 256

// valueMemo is the values MemoValue keeps for the text of version key.
type valueMemo struct {
	Key  changesKey
	Vals map[any]any
}

// splitMemo is the saved text split into lines.
type splitMemo struct {
	Ok    bool
	Gen   uint64
	Lines []string
}

type changesKey struct{ Gen, Ver, Saved uint64 }

// changesMemo is the change marks MemoChanges computed last.
type changesMemo struct {
	Ok          bool
	Key         changesKey
	Bar, Scroll map[int]bool
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
	if m := &d.SavedSplit; !m.Ok || m.Gen != d.SavedGen {
		m.Lines, m.Gen, m.Ok = strings.Split(d.Saved, "\n"), d.SavedGen, true
	}
	return d.SavedSplit.Lines
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
	case d.ext.Virtual:
		return "*" + d.ext.Name + "*"
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
	return d.Dirty() || d.holdsStash()
}

// dirty reports whether saving d would write something: edits to the text, or a new
// encoding or line ending. A stash that is held does not count, since the text on
// screen is not it.
func (d *Document) Dirty() bool {
	return !d.ext.Virtual && (d.Buffer.Modified() || d.MetaDirty)
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
	if disk.Special {
		return nil, fmt.Errorf("open %s: not a regular file", path)
	}
	if disk.Exists && spec.LargeConf.IsLarge(disk.Size) {
		return readLargeDocument(path, disk, spec.LargeConf)
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		d := newDocument(path, "")
		d.Eol = spec.Eol
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
	d.Eol = eol
	d.Ends = lineEnds(decoded)
	d.Disk = disk
	d.Codec = codec
	d.encoding = encodingLabel(codec, text)
	return d, nil
}

// fileText returns text as it is written to disk: each "\n" becomes the
// document's line ending.
func (d *Document) fileText(text string) string {
	if d.Ends != nil {
		return d.mixedText(text)
	}
	if d.Eol == eolCRLF {
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
// and the status after the save says so (see Document.SavedStatus).
func (d *Document) TidyForSave(o Settings) {
	if !o.TrimTrailingWhitespace && !o.EnsureFinalNewline {
		return
	}
	if d.large != nil && !d.Index().Complete {
		return
	}
	before := d.ContentMark()
	d.Buffer.Group(func() {
		if o.TrimTrailingWhitespace && d.large == nil {
			d.trimTrailingWhitespace()
		}
		if o.EnsureFinalNewline {
			d.ensureFinalNewline()
		}
	})
	if d.large != nil && d.Buffer.Refusal() != nil {
		d.large.TidyNote = "Saved without final newline: last line not editable"
	}
	if d.ContentMark() != before {
		// The edit is the newest undo group now, so a multi-caret record is stale.
		d.LastMulti = nil
	}
	if caret := d.Buffer.Clamp(d.Cursor); caret != d.Cursor {
		d.Cursor = caret
		d.Want = -1
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
func (d *Document) Move(p buffer.Pos, extend bool) {
	d.Buffer.Break()
	d.virtual = 0
	d.virtualLines = 0
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
func (d *Document) DeleteSelection() bool {
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
	if d.CodecName() != defaultCodec || d.Path == "" {
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
func (d *Document) LineEnding() string { return d.Eol }

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
