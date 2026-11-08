package editor

import (
	"context"
	"errors"
	"fmt"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/session"
)

// indexBatchBytes is how much of a file one index job reads: a job ends after the
// chunk that takes it to this many bytes.
const indexBatchBytes = 32 << 20

// largeDoc is the state a document in large file mode has beyond its buffer: the
// background scan that builds the line index, and the work that waits for the index.
type largeDoc struct {
	// size is the size of the file when it was opened. estLine is the line length
	// that the line count estimate assumes before enough lines are known.
	size    int64
	estLine int
	// batch is the bytes one IndexJob reads.
	batch int64
	// notes are the extra sentences of the open status (see leftoverTemps).
	notes []string
	// scan is the scan that builds the index, or nil when none has started or the
	// index is complete. While inFlight is set an IndexJob owns it and the editor
	// must not touch it.
	scan     *buffer.IndexScan
	inFlight bool
	// indexErr is the error that stopped the scan, or nil. The scan starts again
	// only after the scan_line_index action cleared it.
	indexErr error
	// pending is the jump that waits for the index, or nil. See pending.go.
	pending *pending
	// rebaseFailed is set when a save wrote the file but the buffer could not move
	// onto the new file. The buffer then still reads the old file, which is now
	// unlinked, and holds the edits as an overlay.
	rebaseFailed bool
	// written describes the file of the last save that the buffer did not move onto: the
	// edits of the buffer are a delta over the old file, and the stash must be a delta
	// over this one. It is nil when the buffer reads the file at the path.
	written *writtenFile
	// saveNote is what the status says after the last save, in place of "Saved". It is
	// empty when the save needs no note. See savedStatus.
	saveNote string
	// noted is the last error of the source that the status said, so the disk poll does
	// not say it again.
	noted error
	// tidyNote is why tidyForSave left something undone, or empty.
	tidyNote string
	// mtime and skip are the modification time (UnixNano) and byte-order mark length of
	// the file the buffer reads, with size: the identity that a stash of unsaved edits
	// must fit (see large_stash.go).
	mtime, skip int64
	// stashDir is where a stash of the unsaved edits is written when the file changes
	// under the document. It is empty when there is no session directory.
	stashDir string
	// stashedVer is the version of the text that was last stashed, when stashedOK is set.
	stashedVer uint64
	stashedOK  bool
	stashNote  string
	// restore is a stash of unsaved edits that waits for the line index, and restoreErr
	// is why it could not be put back, when it could not.
	restore    *session.OverlayStash
	restoreErr error
}

// writtenFile is a file a large document saved and still does not read: the identity
// of the file (as a stash needs it), and the text of the buffer it was written from.
type writtenFile struct {
	buf         *buffer.Buffer
	state       *buffer.TextState
	size, mtime int64
	skip        int64
}

// IndexJob is one step of the background scan that builds the line index of a large
// document. PlanIndex creates it on the editor's goroutine, Run does the reading
// anywhere, and ApplyIndex records the result on the editor's goroutine.
type IndexJob struct {
	doc   *Document
	buf   *buffer.Buffer
	scan  *buffer.IndexScan
	batch int64
}

// IndexResult is what IndexJob.Run read. It is applied by Editor.ApplyIndex.
type IndexResult struct {
	doc   *Document
	buf   *buffer.Buffer
	scan  *buffer.IndexScan
	batch buffer.IndexBatch
	err   error
}

// PlanIndex returns the next step of the scan that builds a line index, and marks its
// document as being scanned. It picks the active document first, then the others in
// tab order, from those in large file mode with an incomplete index, no step running,
// and no error from the last step. A preview tab is not scanned. ok is false when
// there is nothing to scan. It runs on the editor's goroutine, and so does the
// ApplyIndex that must follow every job.
func (e *Editor) PlanIndex() (job IndexJob, ok bool) {
	order := make([]*Document, 0, len(e.docs))
	if d := e.Active(); d != nil {
		order = append(order, d)
	}
	for _, d := range e.docs {
		if d != e.Active() {
			order = append(order, d)
		}
	}
	for _, d := range order {
		l := d.large
		if l == nil || l.inFlight || l.indexErr != nil || d.preview || d.Index().Complete {
			continue
		}
		if l.scan == nil {
			l.scan = d.Buffer.BeginIndexScan()
		}
		if l.scan == nil {
			continue
		}
		l.inFlight = true
		return IndexJob{doc: d, buf: d.Buffer, scan: l.scan, batch: l.batch}, true
	}
	return IndexJob{}, false
}

// Run reads the next chunks of the file, up to 32 MiB, and counts their line breaks.
// It touches only the job's scan, so it may run on another goroutine. ctx stops it
// between chunks; the chunks it read are still reported.
func (j IndexJob) Run(ctx context.Context) IndexResult {
	batch, err := j.scan.Next(ctx, j.batch)
	return IndexResult{doc: j.doc, buf: j.buf, scan: j.scan, batch: batch, err: err}
}

// ApplyIndex adds what an IndexJob read to the line index of its document, and returns
// whether another job is needed. A result for a document that is closed, or whose
// buffer was replaced, is dropped. A read error stops the scan, shows in the
// document's status, and leaves the lines that were indexed; the scan_line_index
// action starts it again. A scan that ctx stopped also ends, and PlanIndex starts it again.
//
// The work that waited for the index runs here: a jump to a line the index now
// reaches, and an action that needed the whole index (see pending.go). When the index
// is complete the status says so. It runs on the editor's goroutine.
func (e *Editor) ApplyIndex(r IndexResult) (more bool) {
	d := r.doc
	if d == nil || d.large == nil || d.Buffer != r.buf || d.large.scan != r.scan || e.indexOfDoc(d) < 0 {
		r.scan.Close()
		return false
	}
	l := d.large
	l.inFlight = false
	d.Buffer.ApplyIndex(r.batch)
	restoreNote := e.continueRestore(d)
	info := d.Index()
	switch {
	case errors.Is(r.err, context.Canceled), errors.Is(r.err, context.DeadlineExceeded):
		return false
	case r.err != nil:
		l.indexErr = r.err
		l.scan.Close()
		l.scan = nil
		// A source that changed under the scan shows as such from now on.
		_ = d.CheckSource()
		l.pending = nil
		d.fail(fmt.Errorf("Index failed: %w", r.err))
		if note := d.stashUnsaved(); note != "" {
			d.status += ". " + note
			d.statusErr = d.status
		}
		return false
	case info.Complete:
		l.scan.Close()
		l.scan = nil
		d.status = lineIndexStatus(info.Lines)
		if restoreNote != "" {
			d.status += ". " + restoreNote
		}
	case l.pending != nil:
		d.status = scanningStatus(info)
	}
	e.resolvePending(d)
	return !info.Complete
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

// scanLineIndex reports the line index of the document. A document in memory has built
// its index as it was read, so the status gives the real line count of the text. A
// large document reports the progress of its scan, or the line count once the scan
// is done. After a scan stopped on an error it starts the scan again: the next
// PlanIndex returns a job.
func scanLineIndex(e *Editor, d *Document, _ map[string]any) error {
	info := d.Index()
	switch {
	case info.Complete:
		e.setStatus(lineIndexStatus(info.Lines))
	case d.large != nil && d.large.indexErr != nil:
		d.large.indexErr = nil
		e.setStatus("Retrying line index... " + scanningStatus(info))
	default:
		e.setStatus(scanningStatus(info))
	}
	return nil
}
