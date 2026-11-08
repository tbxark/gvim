package document

import (
	"context"
	"errors"
	"time"
)

// FileAccess is the file I/O seam of a document: nil means the local filesystem (the
// historical path), non-nil means every read, stat and save of the document goes through
// it. The remote authority implements it, and nothing in the document falls back to the
// local filesystem once it is set: a remote document can only be read and written on the
// remote host. The interface is synchronous because the editor already runs document
// loads and saves in commands (off the update loop).
type FileAccess interface {
	// ReadFile reads at most max bytes of path. A missing path reports fs.ErrNotExist.
	ReadFile(path string, max int64) ([]byte, error)
	// Stat reports the path state; a missing path is the zero AccessStat with a nil
	// error, matching how diskStateOf treats a stat failure.
	Stat(path string) (AccessStat, error)
	// WriteFileAtomic writes data to path atomically (a temporary file in the same
	// directory, then a rename), the way document atomic saves work locally.
	WriteFileAtomic(path string, data []byte) error
}

// AccessStat is what a FileAccess reports about one path.
type AccessStat struct {
	Exists bool
	// Special marks a path that is neither a regular file nor a directory (a FIFO, a
	// device, a socket): opening one can block, so the document does not read it.
	Special bool
	// Dir marks a directory: the editor refuses to open one as a buffer instead of
	// attempting a read that a remote helper would reject.
	Dir   bool
	Size  int64
	Mtime time.Time
}

// diskState converts the access answer to the document's disk state.
func (a AccessStat) diskState() diskState {
	return diskState{Exists: a.Exists, Special: a.Special, Size: a.Size, Mtime: a.Mtime}
}

// WriteStream is one open streamed write of a file that is larger than memory. The
// document walks its windowed source in bounded chunks and calls Write for each; Commit
// applies the mode and renames the temporary file over the path, Abort removes it. The
// original file is untouched until Commit, so a failed or cancelled save changes nothing.
type WriteStream interface {
	// Write sends one bounded chunk (the backend accepted all of it when it returns).
	Write(ctx context.Context, p []byte) error
	// Commit completes the atomic replacement. The temporary file is gone afterwards.
	Commit(ctx context.Context) error
	// Abort removes the temporary file; safe after Commit/Abort. It must not touch the
	// target and should run even when the save context was cancelled.
	Abort(ctx context.Context)
}

// ErrStreamUnsupported means the file access has the StreamAccess method but its backend
// cannot stream this write: the save keeps the bounded whole-file fallback instead of
// failing. It is the only StreamAccess error a save falls back on; a real open failure
// (permission, carrier) still fails the save.
var ErrStreamUnsupported = errors.New("streamed atomic write is not supported by this file access")

// StreamAccess is the optional FileAccess part for streamed atomic writes. A save of a
// windowed large remote file uses it to stream the merged file with bounded memory; an
// access without it keeps the bounded whole-file fallback.
type StreamAccess interface {
	// WriteStream opens a streamed atomic write to path.
	WriteStream(ctx context.Context, path string) (WriteStream, error)
}

// SetFileAccess installs (or clears, with nil) the document's I/O seam. The app uses it
// when a remote authority connects after the document exists; a document created with a
// load spec already carries the spec's access.
func (d *Document) SetFileAccess(a FileAccess) { d.access = a }

// FileAccess returns the document's file I/O seam, or nil when it uses the local
// filesystem. Callers use it to route a disk check or a reload through the same seam as
// the load and the save.
func (d *Document) FileAccess() FileAccess { return d.access }

// DiskState reports the current state of the document's file through its file I/O seam.
// A remote document stat'ed this way never consults the local filesystem, so a remote
// path that does not exist locally is not mistaken for a deleted file.
func (d *Document) DiskState() DiskState {
	if d.access != nil {
		st, err := d.access.Stat(d.Path)
		if err != nil {
			return DiskState{}
		}
		return st.diskState()
	}
	return diskStateOf(d.Path)
}

// errNoRemoteLarge refuses a remote large-file save: large-file mode streams the file
// from the local disk, and a remote document is always loaded whole through its access
// (a recorded residual of the remote routing).
var errNoRemoteLarge = errors.New("large-file mode is not available for a remote document")
