package authority

// Streamed atomic writes: the file port's optional surface for saving a file larger than
// memory. The caller walks its source in bounded chunks and calls Write for each chunk; the
// backend writes them into a temporary file next to the target (on the remote host, through
// the same helper channel as the other file operations) and Commit applies the mode and
// renames the temporary file over the target. Abort removes the temporary file. The
// original file is untouched until Commit, so a failed or cancelled write changes nothing.

import (
	"context"
	"fmt"

	"github.com/tbxark/gvim/plugin/hostos"
)

// WriteStream is one open streamed write to a path. It is not safe for concurrent use.
// Write must be called before Commit/Abort; the caller aborts on any error or cancellation.
type WriteStream interface {
	// Write sends one bounded chunk. It returns only after the backend accepted the whole
	// chunk (a failed append is an error); p is not retained after the call returns.
	Write(ctx context.Context, p []byte) error
	// Commit applies the mode and renames the temporary file over the target. On error the
	// temporary file is removed and the target is untouched.
	Commit(ctx context.Context) error
	// Abort removes the temporary file. It is safe after Commit/Abort and never touches the
	// target; a cancelled context does not skip the cleanup.
	Abort(ctx context.Context)
}

// WriteStreamer is the optional part of a FileSystem that supports streamed atomic
// writes. A backend without it keeps the whole-file WriteFile path; the caller asks with a
// type assertion (see WriteStreams) and reports ErrUnsupportedFileIO when there is none.
type WriteStreamer interface {
	// WriteStream opens a streamed atomic write to path. Only atomic, non-appending writes
	// are supported: the stream creates its own temporary file and renames it at Commit.
	WriteStream(ctx context.Context, path string, o hostos.WriteOptions) (WriteStream, error)
}

// WriteStreams returns the streamed-write surface of fs, or nil when the backend has none.
func WriteStreams(fs FileSystem) WriteStreamer {
	s, _ := fs.(WriteStreamer)
	return s
}

var _ WriteStreamer = (*sshFiles)(nil)
var _ WriteStreamer = (*local)(nil)

// validateStreamOptions rejects the write options a stream cannot honor. A stream is always
// an atomic replacement (its whole point is a temporary file renamed over the target), so
// append and parents are refused instead of silently differing from WriteFile.
func validateStreamOptions(o hostos.WriteOptions) error {
	if o.Append {
		return fmt.Errorf("%w: a streamed write cannot append", ErrUnsupportedFileIO)
	}
	if !o.Atomic {
		return fmt.Errorf("%w: a streamed write is atomic only", ErrUnsupportedFileIO)
	}
	if o.Parents {
		return fmt.Errorf("%w: a streamed write does not create parent directories", ErrUnsupportedFileIO)
	}
	return nil
}
