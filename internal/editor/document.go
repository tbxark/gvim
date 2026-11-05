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
	Path   string
	Buffer *buffer.Buffer
	Cursor buffer.Pos
	// Sel is nil when nothing is selected. Its Head always equals Cursor.
	Sel *buffer.Selection
	// Top is the index of the first line shown in the editor pane.
	Top int
	// preview marks a tab opened by OpenPreview. A preview tab is replaced by
	// the next preview and becomes permanent once it is edited or opened again
	// with OpenFile.
	preview bool

	// saved is the text as it was at open or the last save. It is the only
	// saved-state record: SavedLines and LinesChangedSinceSave read it.
	saved string
	// encoding is "ASCII" or "UTF-8" for the saved text, computed on open and
	// on save so that rendering never scans the text.
	encoding string

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
}

// newDocument returns a Document holding text at path. An empty path means
// an unnamed buffer.
func newDocument(path, text string) *Document {
	return &Document{Path: path, Buffer: buffer.New(text), saved: text, encoding: encodingOf(path, text), want: -1}
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

// ErrNotUTF8 is returned by OpenFile when a file is not valid UTF-8.
var ErrNotUTF8 = errors.New("file is not valid UTF-8")

// ErrNoPath is returned by Save when the active buffer has no file name. The
// caller runs Save As, which names the buffer and writes it.
var ErrNoPath = errors.New("buffer has no file name")

// readDocument loads path. A missing file yields an empty document that will
// create the file on save, as an editor does for a new path.
func readDocument(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return newDocument(path, ""), nil
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("open %s: %w", path, ErrNotUTF8)
	}
	return newDocument(path, string(data)), nil
}

// save writes the buffer to its path and marks it saved. An untitled buffer
// returns ErrNoPath. The bytes are written exactly as the buffer holds them.
func (d *Document) save() error {
	if d.Path == "" {
		return ErrNoPath
	}
	return d.writeTo(d.Path)
}

// writeTo writes the buffer to path atomically (see writeFileAtomic) and marks
// it saved. It does not change d.Path, so a caller that wants a new path sets
// it only after success. On failure the buffer stays modified.
func (d *Document) writeTo(path string) error {
	text := d.Buffer.Text()
	if err := writeFileAtomic(path, []byte(text)); err != nil {
		return fmt.Errorf("save %s: %w", path, err)
	}
	d.Buffer.MarkSaved()
	d.saved = text
	d.encoding = encodingOf(path, text)
	return nil
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

// Encoding reports the encoding of the saved text: "UTF-8" for an unnamed
// buffer (the editor's startup buffer), "ASCII" for a named file whose saved
// text has no byte above 0x7f, and "UTF-8" otherwise.
func (d *Document) Encoding() string { return d.encoding }

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
