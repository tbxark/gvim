package editor

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/tbxark/gvim/internal/buffer"
)

// A large document is saved by streaming a snapshot of its buffer into a temporary file
// next to the target, which is then renamed over it. The text is never held whole. The
// lines the user did not edit are copied from the old file byte for byte, and the lines
// that were edited are encoded. When the write is done the buffer moves onto the new file
// without scanning it again (buffer.Rebase), and its undo history is gone.

// saveBufferSize is the size of the buffer between the snapshot and the temporary file.
const saveBufferSize = 1 << 20

// errFileChanged is the refusal of a save of a large document whose file changed on disk
// under it. Nothing more can be read from the file safely, so nothing is written.
var errFileChanged = &largeError{
	msg:  "File changed on disk: reload or Save As after reloading",
	errs: []error{buffer.ErrSourceChanged},
}

// errFileGrew is the refusal of a save over a file that grew while the document was
// open. The new bytes are not in the document, and the rename would drop them.
var errFileGrew = &largeError{
	msg:  "File grew on disk: Save As to another path or reload",
	errs: []error{buffer.ErrSourceGrew},
}

// largeSave is what the save of a large document carries from the plan to the write and
// to the record of the result.
type largeSave struct {
	// buf is the buffer the snapshot was taken from, and ver its version then. The
	// result is adopted only by the buffer that is still at that version.
	buf *buffer.Buffer
	ver uint64
	// state is the text of buf at ver, to describe later edits over the file written.
	state *buffer.TextState
	// snap is the text to write. Run releases it.
	snap *buffer.Snapshot
	// eol ends a line the user typed, and prefix is the byte-order mark to write first.
	eol    string
	prefix []byte
	// noop is set when the document has no changes and is saved to its own file: the
	// write is skipped as long as the file on disk is as the document left it.
	noop bool
	// allowGrown lets the write go on over a source that grew. It is set for a Save As to
	// another path only, which writes the bytes the document has and leaves the rest.
	allowGrown bool
	// srcPath and srcSize are the file the document reads and its size then, for the
	// note about the bytes a Save As leaves behind.
	srcPath string
	srcSize int64
	// note is what the status says after the save, when something was left undone.
	note string
}

// planSaveLarge prepares the save of a large document to path. Nothing in it reads the
// text. The order is binding. A source that changed refuses every save. A source that
// grew refuses the save over itself. An index that is not complete refuses the save,
// because the end of the document is not known. A document with no changes saved to its
// own file is a no-op. Only then are the save-time edits made, and the snapshot is taken
// after them, so it holds the text that is written.
func (d *Document) planSaveLarge(path string, o Options, job SaveJob) SaveJob {
	if d.restoring() {
		// The text on screen is the file as it is on disk until the stash is back, and a
		// save now would write that and look like the end of the unsaved changes.
		job.err = fmt.Errorf("save %s: %w", path, ErrRestoring)
		return job
	}
	_ = d.CheckSource()
	switch err := d.Buffer.SourceErr(); {
	case errors.Is(err, buffer.ErrSourceChanged):
		job.err = errFileChanged
	case err != nil:
		job.err = fmt.Errorf("save %s: %w", path, err)
	}
	if job.err != nil {
		return job
	}
	own := sameFile(path, d.Path)
	grew := d.Buffer.SourceGrew()
	switch info := d.Index(); {
	case grew && own:
		job.err = errFileGrew
	case d.large.indexErr != nil:
		job.err = fmt.Errorf("save %s: line index failed: %w", path, d.large.indexErr)
	case !info.Complete:
		job.err = &largeError{
			msg:  scanningStatus(info) + ": save when the index is done",
			errs: []error{ErrIndexing},
		}
	}
	if job.err != nil {
		return job
	}
	lg := &largeSave{
		buf: d.Buffer, eol: "\n", noop: own && !d.dirty(),
		allowGrown: grew, srcPath: d.Path, srcSize: d.large.size,
	}
	if d.eol == eolCRLF {
		lg.eol = "\r\n"
	}
	if d.codecName() == "utf-8-bom" {
		lg.prefix = utf8BOM
	}
	if !lg.noop {
		d.large.tidyNote = ""
		d.tidyForSave(o)
		lg.note = d.large.tidyNote
	}
	lg.ver = d.Buffer.Version()
	lg.snap = d.Buffer.Snapshot()
	lg.state, _ = d.Buffer.TextState()
	job.label = d.encoding
	job.large = lg
	return job
}

// writeLarge streams the snapshot of the job into its file.
func (j SaveJob) writeLarge(ctx context.Context) SaveResult {
	lg := j.large
	opts := buffer.WriteOptions{EOL: lg.eol, Prefix: lg.prefix, AllowGrown: lg.allowGrown}
	var res buffer.WriteResult
	var tmp os.FileInfo
	owner, err := writeFileAtomicStream(j.path, func(w io.Writer) error {
		bw := bufio.NewWriterSize(w, saveBufferSize)
		var err error
		if res, err = lg.snap.WriteTo(ctx, bw, opts); err != nil {
			return err
		}
		return bw.Flush()
	}, func(fi os.FileInfo) { tmp = fi })
	switch {
	case errors.Is(err, buffer.ErrSourceChanged):
		return SaveResult{job: j, err: errFileChanged}
	case errors.Is(err, buffer.ErrSourceGrew):
		return SaveResult{job: j, err: errFileGrew}
	case err != nil:
		return SaveResult{job: j, err: fmt.Errorf("save %s: %w", j.path, err)}
	}
	r := SaveResult{job: j, disk: diskStateOf(j.path), res: res, tmp: tmp, note: lg.note}
	if lg.allowGrown {
		if now := diskStateOf(lg.srcPath); now.exists && now.size > lg.srcSize {
			r.note = fmt.Sprintf("Saved without %d appended bytes", now.size-lg.srcSize)
		}
	}
	r.note = saveNote(r.note, owner)
	return r
}

// saveNote is the status after a save whose note is note and which could not keep
// what owner says (see writeFileAtomic).
func saveNote(note, owner string) string {
	switch {
	case owner == "":
		return note
	case note == "":
		return "Saved; " + owner
	}
	return note + "; " + owner
}

// applyLargeSave records the result of the write of a large document on the editor's
// goroutine. A document that was edited while the file was written stays modified: the
// file holds the text of the snapshot, and the next save writes the newer text. A
// document that has not changed moves onto the file that was written. If the buffer
// cannot, the file is saved anyway, the buffer keeps reading the file it has, and the
// status says the document should be reloaded.
func (e *Editor) applyLargeSave(r SaveResult) {
	d, lg := r.job.doc, r.job.large
	if d.large == nil || d.Buffer != lg.buf {
		// The document was reverted or closed while the file was written.
		return
	}
	l := d.large
	if r.skipped {
		l.saveNote = "Nothing to save"
		return
	}
	d.disk = r.disk
	d.encoding = r.job.label
	l.saveNote = r.note
	if d.Buffer.Version() != lg.ver {
		// The buffer goes on reading the old file, which is not the file at the path now.
		l.written = writtenOf(r)
		return
	}
	hadHistory := d.Buffer.Stats().HistoryGroups > 0
	d.metaDirty = false
	if !e.rebaseOnSaved(d, r) {
		d.Buffer.MarkSaved()
		if r.tmp != nil {
			// The file at the path is not the one that was written, or could not be
			// told to be: record the file that was written, so that the next save sees
			// the difference.
			d.disk = diskState{exists: true, size: r.tmp.Size(), mtime: r.tmp.ModTime()}
		}
		l.rebaseFailed = true
		l.written = writtenOf(r)
		l.saveNote = "Saved; reload to continue editing"
		return
	}
	l.rebaseFailed, l.written = false, nil
	d.resetHistoryState()
	if hadHistory && l.saveNote == "" {
		l.saveNote = "Saved (undo history cleared)"
	}
}

// writtenOf describes the file that the save of r wrote. Its modification time is the
// one of the temporary file that was renamed to the path.
func writtenOf(r SaveResult) *writtenFile {
	lg := r.job.large
	mtime := mtimeNano(r.disk)
	if r.tmp != nil {
		mtime = r.tmp.ModTime().UnixNano()
	}
	return &writtenFile{
		buf: lg.buf, state: lg.state, size: r.res.Bytes, mtime: mtime, skip: int64(len(lg.prefix)),
	}
}

// rebaseOnSaved makes the buffer of d read the file that was just written, after it
// checks that the file at the path is the temporary file that was renamed there: the
// same file, of the size that was written. It reports whether the buffer moved. On a
// false nothing has changed.
func (e *Editor) rebaseOnSaved(d *Document, r SaveResult) bool {
	lg := r.job.large
	open := openSavedSource
	if e.opts.openSrc != nil {
		open = func(path string) (buffer.Source, os.FileInfo, error) {
			src, err := e.opts.openSrc.open(path)
			return src, nil, err
		}
	}
	src, info, err := open(r.job.path)
	if err != nil {
		return false
	}
	if info != nil && (r.tmp == nil || !os.SameFile(info, r.tmp) || info.Size() != r.res.Bytes) {
		_ = src.Close()
		return false
	}
	if err := d.Buffer.Rebase(src, r.res, lg.ver, int64(len(lg.prefix))); err != nil {
		_ = src.Close()
		return false
	}
	d.large.size = src.Size()
	d.large.mtime = mtimeNano(r.disk)
	d.large.skip = int64(len(lg.prefix))
	return true
}

// resetHistoryState drops what refers to the undo history or to places of the text that
// a new buffer does not have: the record of the last multi-caret edit, the range the
// find actions are limited to, and the work that waits for the line index. The jump list
// and the bookmarks need nothing: every jump clamps its place into the text.
func (d *Document) resetHistoryState() {
	d.lastMulti = nil
	d.findRange = nil
	if d.large != nil {
		d.large.pending = nil
	}
}

// savedStatus is the status after a save: "Saved", or the note of a large document
// that has one. The note is used once.
func (d *Document) savedStatus() string {
	if d.large != nil && d.large.saveNote != "" {
		note := d.large.saveNote
		d.large.saveNote = ""
		return note
	}
	if d.saveNote != "" {
		note := d.saveNote
		d.saveNote = ""
		return note
	}
	return "Saved"
}
