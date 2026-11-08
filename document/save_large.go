package document

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/tbxark/gvim/atomicfile"
	"github.com/tbxark/gvim/buffer"
	"io"
	"os"
)

// saveBufferSize is the size of the buffer between the snapshot and the temporary file.
const saveBufferSize = 1 << 20

// errFileChanged is the refusal of a save of a large document whose file changed on disk
// under it. Nothing more can be read from the file safely, so nothing is written.
var errFileChanged = &largeError{
	Msg:  "File changed on disk: reload or Save As after reloading",
	Errs: []error{buffer.ErrSourceChanged},
}

// errFileGrew is the refusal of a save over a file that grew while the document was
// open. The new bytes are not in the document, and the rename would drop them.
var errFileGrew = &largeError{
	Msg:  "File grew on disk: Save As to another path or reload",
	Errs: []error{buffer.ErrSourceGrew},
}

// largeSave is what the save of a large document carries from the plan to the write and
// to the record of the result.
type largeSave struct {
	// buf is the buffer the snapshot was taken from, and ver its version then. The
	// result is adopted only by the buffer that is still at that version.
	Buf *buffer.Buffer
	Ver uint64
	// state is the text of buf at ver, to describe later edits over the file written.
	State *buffer.TextState
	// snap is the text to write. Run releases it.
	Snap *buffer.Snapshot
	// eol ends a line the user typed, and prefix is the byte-order mark to write first.
	Eol    string
	Prefix []byte
	// noop is set when the document has no changes and is saved to its own file: the
	// write is skipped as long as the file on disk is as the document left it.
	Noop bool
	// allowGrown lets the write go on over a source that grew. It is set for a Save As to
	// another path only, which writes the bytes the document has and leaves the rest.
	AllowGrown bool
	// srcPath and srcSize are the file the document reads and its size then, for the
	// note about the bytes a Save As leaves behind.
	SrcPath string
	SrcSize int64
	// note is what the status says after the save, when something was left undone.
	Note string
}

// planSaveLarge prepares the save of a large document to path. Nothing in it reads the
// text. The order is binding. A source that changed refuses every save. A source that
// grew refuses the save over itself. An index that is not complete refuses the save,
// because the end of the document is not known. A document with no changes saved to its
// own file is a no-op. Only then are the save-time edits made, and the snapshot is taken
// after them, so it holds the text that is written.
func (d *Document) planSaveLarge(path string, o Settings, job SaveJob) SaveJob {
	if d.Restoring() {
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
	case d.large.IndexErr != nil:
		job.err = fmt.Errorf("save %s: line index failed: %w", path, d.large.IndexErr)
	case !info.Complete:
		job.err = &largeError{
			Msg:  scanningStatus(info) + ": save when the index is done",
			Errs: []error{ErrIndexing},
		}
	}
	if job.err != nil {
		return job
	}
	lg := &largeSave{
		Buf: d.Buffer, Eol: "\n", Noop: own && !d.Dirty(),
		AllowGrown: grew, SrcPath: d.Path, SrcSize: d.large.Size,
	}
	if d.Eol == eolCRLF {
		lg.Eol = "\r\n"
	}
	if d.CodecName() == "utf-8-bom" {
		lg.Prefix = utf8BOM
	}
	if !lg.Noop {
		d.large.TidyNote = ""
		d.TidyForSave(o)
		lg.Note = d.large.TidyNote
	}
	lg.Ver = d.Buffer.Version()
	lg.Snap = d.Buffer.Snapshot()
	lg.State, _ = d.Buffer.TextState()
	job.Label = d.encoding
	job.large = lg
	return job
}

// writeLarge streams the snapshot of the job into its file.
func (j SaveJob) writeLarge(ctx context.Context) SaveResult {
	lg := j.large
	opts := buffer.WriteOptions{EOL: lg.Eol, Prefix: lg.Prefix, AllowGrown: lg.AllowGrown}
	var res buffer.WriteResult
	var tmp os.FileInfo
	owner, err := atomicfile.WriteFileStream(j.Path, func(w io.Writer) error {
		bw := bufio.NewWriterSize(w, saveBufferSize)
		var err error
		if res, err = lg.Snap.WriteTo(ctx, bw, opts); err != nil {
			return err
		}
		return bw.Flush()
	}, func(fi os.FileInfo) { tmp = fi })
	switch {
	case errors.Is(err, buffer.ErrSourceChanged):
		return SaveResult{Job: j, err: errFileChanged}
	case errors.Is(err, buffer.ErrSourceGrew):
		return SaveResult{Job: j, err: errFileGrew}
	case err != nil:
		return SaveResult{Job: j, err: fmt.Errorf("save %s: %w", j.Path, err)}
	}
	r := SaveResult{Job: j, Disk: diskStateOf(j.Path), Res: res, Tmp: tmp, Note: lg.Note}
	if lg.AllowGrown {
		if now := diskStateOf(lg.SrcPath); now.Exists && now.Size > lg.SrcSize {
			r.Note = fmt.Sprintf("Saved without %d appended bytes", now.Size-lg.SrcSize)
		}
	}
	r.Note = saveNote(r.Note, owner)
	return r
}

// errRemoteLargeSaveTooBig is the refusal of a windowed large-file save whose merged
// content is over remoteLargeSaveMax.
var errRemoteLargeSaveTooBig = errors.New("remote large-file save: merged content is over the windowed-save bound")

// limitedSaveWriter collects the merged bytes of a remote large save under a bound.
type limitedSaveWriter struct {
	buf bytes.Buffer
	max int64
}

func (w *limitedSaveWriter) Write(p []byte) (int, error) {
	if int64(w.buf.Len())+int64(len(p)) > w.max {
		return 0, errRemoteLargeSaveTooBig
	}
	return w.buf.Write(p)
}

// writeLargeRemote saves a remote large document: the snapshot merges the overlay
// with the unchanged ranges, which are read through the window source in bounded
// chunks (Snapshot.WriteTo loads a missed window while it runs), and the whole merged
// file is written with the authority's atomic write. The merge is bounded because the
// atomic write takes the file in memory. The disk state is checked through the access
// first, so a remote file that changed underneath refuses the save. A fresh source for
// the written file is opened here, in the command, and carried in the result so the
// editor rebase touches no remote I/O on the update goroutine.
func (j SaveJob) writeLargeRemote(ctx context.Context, access FileAccess) SaveResult {
	lg := j.large
	now, err := access.Stat(j.Path)
	if err != nil {
		return SaveResult{Job: j, err: fmt.Errorf("save %s: %w", j.Path, err)}
	}
	disk := now.diskState()
	if j.RefuseExisting && !sameFile(j.OwnPath, j.Path) && disk.Exists {
		return SaveResult{Job: j, err: fmt.Errorf("save as %s: %w", j.Path, ErrExists)}
	}
	if j.CheckDisk && disk.Exists && !disk.SameAs(j.Expect) {
		return SaveResult{Job: j, err: fmt.Errorf("%s %w", j.Name, ErrChangedOnDisk)}
	}
	if lg.Noop && disk.Exists && disk.SameAs(j.Expect) {
		return SaveResult{Job: j, Disk: disk, Skipped: true}
	}
	accessRange, ok := access.(RangeAccess)
	if !ok {
		return SaveResult{Job: j, err: fmt.Errorf("save %s: %w", j.Path, errNoRemoteLarge)}
	}
	opts := buffer.WriteOptions{EOL: lg.Eol, Prefix: lg.Prefix, AllowGrown: lg.AllowGrown}
	if streamer, ok := access.(StreamAccess); ok {
		stream, serr := streamer.WriteStream(ctx, j.Path)
		switch {
		case serr == nil:
			// The streamed path: the snapshot streams the merged file through a temporary
			// file on the backend, so the merged size is not limited by memory.
			return j.writeLargeRemoteStreamed(ctx, access, accessRange, stream, opts)
		case !errors.Is(serr, ErrStreamUnsupported):
			return SaveResult{Job: j, err: fmt.Errorf("save %s: %w", j.Path, serr)}
		}
		// The access advertises streaming but this backend has none: keep the bounded
		// whole-file fallback below.
	}
	lw := &limitedSaveWriter{max: remoteLargeSaveMax}
	res, werr := lg.Snap.WriteTo(ctx, lw, opts)
	switch {
	case errors.Is(werr, errRemoteLargeSaveTooBig):
		return SaveResult{Job: j, err: fmt.Errorf("save %s: %s; the merged file is over the remote windowed-save bound", j.Path, errRemoteLargeSaveTooBig.Error())}
	case errors.Is(werr, buffer.ErrSourceChanged):
		return SaveResult{Job: j, err: errFileChanged}
	case errors.Is(werr, buffer.ErrSourceGrew):
		return SaveResult{Job: j, err: errFileGrew}
	case werr != nil:
		return SaveResult{Job: j, err: fmt.Errorf("save %s: %w", j.Path, werr)}
	}
	if err := access.WriteFileAtomic(j.Path, lw.buf.Bytes()); err != nil {
		return SaveResult{Job: j, err: fmt.Errorf("save %s: %w", j.Path, err)}
	}
	after, err := access.Stat(j.Path)
	if err != nil {
		return SaveResult{Job: j, Disk: diskState{Exists: true, Size: res.Bytes}, Res: res}
	}
	var src buffer.Source
	if fresh, openErr := newRemoteWindowSource(ctx, accessRange, j.Path, int64(lw.buf.Len())); openErr == nil {
		src = fresh
	}
	return SaveResult{Job: j, Disk: after.diskState(), Res: res, Source: src, Note: lg.Note}
}

// writeLargeRemoteStreamed saves a remote large document through a streamed atomic
// write: Snapshot.WriteTo walks the windowed source and the overlay in bounded chunks, so
// the merged file is never materialised in memory. The stream's temporary file stays until
// Commit; an error or cancellation aborts it, leaving the original untouched. The disk
// state was checked before this call, so a changed remote file still refuses the save.
func (j SaveJob) writeLargeRemoteStreamed(ctx context.Context, access FileAccess, accessRange RangeAccess, stream WriteStream, opts buffer.WriteOptions) SaveResult {
	lg := j.large
	cleanup := func() { stream.Abort(context.WithoutCancel(ctx)) }
	res, werr := lg.Snap.WriteTo(ctx, streamChunkWriter{ctx: ctx, stream: stream}, opts)
	switch {
	case errors.Is(werr, buffer.ErrSourceChanged):
		cleanup()
		return SaveResult{Job: j, err: errFileChanged}
	case errors.Is(werr, buffer.ErrSourceGrew):
		cleanup()
		return SaveResult{Job: j, err: errFileGrew}
	case werr != nil:
		cleanup()
		return SaveResult{Job: j, err: fmt.Errorf("save %s: %w", j.Path, werr)}
	}
	if err := stream.Commit(ctx); err != nil {
		cleanup()
		return SaveResult{Job: j, err: fmt.Errorf("save %s: %w", j.Path, err)}
	}
	after, err := access.Stat(j.Path)
	if err != nil {
		return SaveResult{Job: j, Disk: diskState{Exists: true, Size: res.Bytes}, Res: res, Note: lg.Note}
	}
	var src buffer.Source
	if fresh, openErr := newRemoteWindowSource(ctx, accessRange, j.Path, res.Bytes); openErr == nil {
		src = fresh
	}
	return SaveResult{Job: j, Disk: after.diskState(), Res: res, Source: src, Note: lg.Note}
}

// streamChunkWriter adapts a document WriteStream to the io.Writer Snapshot.WriteTo
// expects: every chunk is remote work, so it runs in the save command with the save's
// context.
type streamChunkWriter struct {
	ctx    context.Context
	stream WriteStream
}

func (w streamChunkWriter) Write(p []byte) (int, error) {
	if err := w.stream.Write(w.ctx, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// saveNote is the status after a save whose note is note and which could not keep
// what owner says (see atomicfile.WriteFile).
func saveNote(note, owner string) string {
	switch {
	case owner == "":
		return note
	case note == "":
		return "Saved; " + owner
	}
	return note + "; " + owner
}

// writtenOf describes the file that the save of r wrote. Its modification time is the
// one of the temporary file that was renamed to the path.
func writtenOf(r SaveResult) *writtenFile {
	lg := r.Job.large
	mtime := mtimeNano(r.Disk)
	if r.Tmp != nil {
		mtime = r.Tmp.ModTime().UnixNano()
	}
	return &writtenFile{
		Buf: lg.Buf, State: lg.State, Size: r.Res.Bytes, Mtime: mtime, Skip: int64(len(lg.Prefix)),
	}
}

// resetHistoryState drops what refers to the undo history or to places of the text that
// a new buffer does not have: the record of the last multi-caret edit, the range the
// find actions are limited to, and the work that waits for the line index. The jump list
// and the bookmarks need nothing: every jump clamps its place into the text.
func (d *Document) ResetHistoryState() {
	d.LastMulti = nil
	d.FindRange = nil
	if d.large != nil {
		d.large.Pending = nil
	}
}

// savedStatus is the status after a save: "Saved", or the note of a large document
// that has one. The note is used once.
func (d *Document) SavedStatus() string {
	if d.large != nil && d.large.SaveNote != "" {
		note := d.large.SaveNote
		d.large.SaveNote = ""
		return note
	}
	if d.SaveNote != "" {
		note := d.SaveNote
		d.SaveNote = ""
		return note
	}
	return "Saved"
}

// openSavedSource opens the file at path as the source of a large document that was
// just written there. info is the state of the open file itself, taken from its
// descriptor and not from the path, so the caller can tell which file was opened. It is
// nil for a source that is not a file.
func openSavedSource(path string) (src buffer.Source, info os.FileInfo, err error) {
	src, err = buffer.OpenFileSource(path)
	if err != nil {
		return nil, nil, err
	}
	if st, ok := src.(interface{ Stat() (os.FileInfo, error) }); ok {
		info, _ = st.Stat()
	}
	return src, info, nil
}
