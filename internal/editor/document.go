package editor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/buffer"
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
	// preview marks a tab opened by OpenPreview. A preview tab is replaced by
	// the next preview and becomes permanent once it is edited or opened again
	// with OpenFile.
	preview bool

	// saved is the text as it was at open or the last save. It is the only
	// saved-state record: SavedLines and LinesChangedSinceSave read it.
	saved string
	// encoding is the label of the encoding the saved text is written in (see
	// encodingLabel). It is computed on open, on save and on set_encoding so
	// that rendering never scans the text.
	encoding string
	// eol is the line ending of the saved text, eolLF or eolCRLF. The buffer
	// always holds "\n" as its line break, and fileText converts it back.
	eol string
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
	// status is the message shown while this tab is active. Each tab keeps its
	// own, so switching tabs never shows another tab's message.
	status string
	// codec is the encoding name the text was read with, from internal/encoding.
	// Save encodes with it. "" means utf-8, the encoding of a new document.
	codec string
	// readOnly refuses every change to the text and to the file. See readonly.go.
	readOnly bool
	// findRange limits the find actions to this range when it is set. See
	// doc_search.go.
	findRange *buffer.Selection
	// folded holds the start lines of the folds that are collapsed. See fold.go.
	folded map[int]bool
	// lastMulti is the last multi-caret edit, for undo and redo. See multiEditRecord.
	lastMulti *multiEditRecord
	// view holds the display settings of the document. See viewsettings.go.
	view ViewSettings
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
// outnumber the bare LF breaks, so a tie, or a text with no break, is LF. Bare
// LF breaks in a CRLF file are written as CRLF on save, so the file ends up
// with one ending.
func splitLineEnding(data string) (text, eol string) {
	crlf := strings.Count(data, "\r\n")
	lf := strings.Count(data, "\n") - crlf
	eol = eolLF
	if crlf > lf {
		eol = eolCRLF
	}
	return strings.ReplaceAll(data, "\r\n", "\n"), eol
}

// SavedLines returns the saved text split into lines on '\n', the same way
// buffer.New splits its content. The text is the one at open or at the last
// save. The result is a new slice on every call, so the caller may change it.
// An unnamed, unsaved buffer has the single empty line [""].
func (d *Document) SavedLines() []string {
	return strings.Split(d.saved, "\n")
}

// LinesChangedSinceSave returns the 0-based indexes of lines whose content
// differs from SavedLines: the text at open or at the last save. The
// comparison is line by line, so inserting or deleting a line marks every
// line below it as changed. It returns nil when nothing differs.
func (d *Document) LinesChangedSinceSave() []int {
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

// Modified reports whether the buffer has unsaved edits.
func (d *Document) Modified() bool {
	return d.Buffer.Modified()
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
// path. That document takes defaultEOL, the ending for a new file.
func readDocument(path, defaultEOL string) (*Document, error) {
	// The state is taken before the read. A change between the two then shows
	// as a difference on save, which is the safe side.
	disk := diskStateOf(path)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		d := newDocument(path, "")
		d.eol = defaultEOL
		return d, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	decoded, codec, err := decodeFile(data)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	text, eol := splitLineEnding(decoded)
	d := newDocument(path, text)
	d.eol = eol
	d.disk = disk
	d.codec = codec
	d.encoding = encodingLabel(codec, text)
	return d, nil
}

// fileText returns text as it is written to disk: each "\n" becomes the
// document's line ending.
func (d *Document) fileText(text string) string {
	if d.eol == eolCRLF {
		return strings.ReplaceAll(text, "\n", "\r\n")
	}
	return text
}

// tidyForSave applies the save-time options to the buffer, so the text written
// is the text the buffer holds afterwards. The edits form one undo step. The
// caret and selection are kept inside the text they were on.
func (d *Document) tidyForSave(o Options) {
	if !o.TrimTrailingWhitespace && !o.EnsureFinalNewline {
		return
	}
	before := d.Buffer.Text()
	d.Buffer.Group(func() {
		if o.TrimTrailingWhitespace {
			d.trimTrailingWhitespace()
		}
		if o.EnsureFinalNewline {
			d.ensureFinalNewline()
		}
	})
	if d.Buffer.Text() != before {
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
func (d *Document) Encoding() string { return d.encoding }

// LineEnding is "LF" or "CRLF", the ending of the saved text. It is detected
// when the file is opened, and a new document takes Options.DefaultLineEnding.
func (d *Document) LineEnding() string { return d.eol }

// setLineEnding changes the line ending that the next save writes. The text is
// not changed, so the document keeps its modified state. Its argument "value" is
// "lf" or "crlf". Any other value is an error, and the ending stays as it was.
func setLineEnding(e *Editor, d *Document, args map[string]any) error {
	value, ok := args["value"].(string)
	if !ok {
		return errors.New("set_line_ending: missing string argument \"value\"")
	}
	switch value {
	case "lf":
		d.eol = eolLF
	case "crlf":
		d.eol = eolCRLF
	default:
		return fmt.Errorf("set_line_ending: value %q is not \"lf\" or \"crlf\"", value)
	}
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
