package editor

import (
	"context"
	"errors"
	"fmt"
	"github.com/tbxark/gvim/buffer"
)

// IndexJob is one step of the background scan that builds the line index of a large
// document. PlanIndex creates it on the editor's goroutine, Run does the reading
// anywhere, and ApplyIndex records the result on the editor's goroutine.
type IndexJob struct {
	Doc   *Document
	Buf   *buffer.Buffer
	Scan  *buffer.IndexScan
	Batch int64
}

// IndexResult is what IndexJob.Run read. It is applied by Editor.ApplyIndex.
type IndexResult struct {
	Doc   *Document
	Buf   *buffer.Buffer
	Scan  *buffer.IndexScan
	Batch buffer.IndexBatch
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
		l := d.LargeState()
		if l == nil || l.InFlight || l.IndexErr != nil || d.Preview() || d.Index().Complete {
			continue
		}
		if l.Scan == nil {
			l.Scan = d.Buffer.BeginIndexScan()
		}
		if l.Scan == nil {
			continue
		}
		l.InFlight = true
		return IndexJob{Doc: d, Buf: d.Buffer, Scan: l.Scan, Batch: l.Batch}, true
	}
	return IndexJob{}, false
}

// Run reads the next chunks of the file, up to 32 MiB, and counts their line breaks.
// It touches only the job's scan, so it may run on another goroutine. ctx stops it
// between chunks; the chunks it read are still reported.
func (j IndexJob) Run(ctx context.Context) IndexResult {
	batch, err := j.Scan.Next(ctx, j.Batch)
	return IndexResult{Doc: j.Doc, Buf: j.Buf, Scan: j.Scan, Batch: batch, err: err}
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
	d := r.Doc
	if d == nil || d.LargeState() == nil || d.Buffer != r.Buf || d.LargeState().Scan != r.Scan || e.indexOfDoc(d) < 0 {
		r.Scan.Close()
		return false
	}
	l := d.LargeState()
	l.InFlight = false
	d.Buffer.ApplyIndex(r.Batch)
	restoreNote := e.continueRestore(d)
	info := d.Index()
	switch {
	case errors.Is(r.err, context.Canceled), errors.Is(r.err, context.DeadlineExceeded):
		return false
	case r.err != nil:
		l.IndexErr = r.err
		l.Scan.Close()
		l.Scan = nil
		// A source that changed under the scan shows as such from now on.
		_ = d.CheckSource()
		l.Pending = nil
		//nolint:staticcheck // ST1005: the text is the status row message, which tests assert
		d.Fail(fmt.Errorf("Index failed: %w", r.err))
		if note := d.StashUnsaved(); note != "" {
			d.Status += ". " + note
			d.StatusErr = d.Status
		}
		return false
	case info.Complete:
		l.Scan.Close()
		l.Scan = nil
		d.Status = lineIndexStatus(info.Lines)
		if restoreNote != "" {
			d.Status += ". " + restoreNote
		}
	case l.Pending != nil:
		d.Status = scanningStatus(info)
	}
	e.resolvePending(d)
	return !info.Complete
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
	case d.LargeState() != nil && d.LargeState().IndexErr != nil:
		d.LargeState().IndexErr = nil
		e.setStatus("Retrying line index... " + scanningStatus(info))
	default:
		e.setStatus(scanningStatus(info))
	}
	return nil
}
