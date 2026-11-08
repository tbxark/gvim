package document

import (
	"fmt"
	"github.com/tbxark/gvim/buffer"
)

// indexBatchBytes is how much of a file one index job reads: a job ends after the
// chunk that takes it to this many bytes.
const indexBatchBytes = 32 << 20

// largeDoc is the state a document in large file mode has beyond its buffer: the
// background scan that builds the line index, and the work that waits for the index.
type largeDoc struct {
	// size is the size of the file when it was opened. estLine is the line length
	// that the line count estimate assumes before enough lines are known.
	Size    int64
	EstLine int
	// batch is the bytes one IndexJob reads.
	Batch int64
	// notes are the extra sentences of the open status (see leftoverTemps).
	Notes []string
	// scan is the scan that builds the index, or nil when none has started or the
	// index is complete. While inFlight is set an IndexJob owns it and the editor
	// must not touch it.
	Scan     *buffer.IndexScan
	InFlight bool
	// indexErr is the error that stopped the scan, or nil. The scan starts again
	// only after the scan_line_index action cleared it.
	IndexErr error
	// pending is the jump that waits for the index, or nil. See pending.go.
	Pending *pending
	// rebaseFailed is set when a save wrote the file but the buffer could not move
	// onto the new file. The buffer then still reads the old file, which is now
	// unlinked, and holds the edits as an overlay.
	RebaseFailed bool
	// written describes the file of the last save that the buffer did not move onto: the
	// edits of the buffer are a delta over the old file, and the stash must be a delta
	// over this one. It is nil when the buffer reads the file at the path.
	Written *writtenFile
	// saveNote is what the status says after the last save, in place of "Saved". It is
	// empty when the save needs no note. See savedStatus.
	SaveNote string
	// noted is the last error of the source that the status said, so the disk poll does
	// not say it again.
	Noted error
	// tidyNote is why tidyForSave left something undone, or empty.
	TidyNote string
	// mtime and skip are the modification time (UnixNano) and byte-order mark length of
	// the file the buffer reads, with size: the identity that a stash of unsaved edits
	// must fit (see large_stash.go).
	Mtime, Skip int64
	// RemoteWindowed marks a large document whose bytes are windowed over a remote
	// authority (read-only in this slice). The status row says so.
	RemoteWindowed bool
	// stashDir is where a stash of the unsaved edits is written when the file changes
	// under the document. It is empty when there is no session directory.
	StashDir string
	// stashedVer is the version of the text that was last stashed, when stashedOK is set.
	StashedVer uint64
	StashedOK  bool
	StashNote  string
	// restore is a stash of unsaved edits that waits for the line index, and restoreErr
	// is why it could not be put back, when it could not.
	Restore    *OverlayStash
	RestoreErr error
}

// writtenFile is a file a large document saved and still does not read: the identity
// of the file (as a stash needs it), and the text of the buffer it was written from.
type writtenFile struct {
	Buf         *buffer.Buffer
	State       *buffer.TextState
	Size, Mtime int64
	Skip        int64
}

// lineIndexStatus is the status row text of a finished line index.
func lineIndexStatus(lines int) string {
	noun := "lines"
	if lines == 1 {
		noun = "line"
	}
	return fmt.Sprintf("Line index built: %d %s", lines, noun)
}

// scanningStatus is the status row text of an index that is still being built, with
// the share of the file scanned, as Fresh shows it.
func scanningStatus(info buffer.IndexInfo) string {
	pct := 0
	if info.Size > 0 {
		pct = int(min(info.Indexed*100/info.Size, 99))
	}
	return fmt.Sprintf("Scanning... %d%%", pct)
}
