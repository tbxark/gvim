package editor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/tbxark/gvim/internal/buffer"
)

// SaveJob is one file write of a save, prepared on the editor's goroutine by
// PlanAutoSave, PlanSave, PlanSaveAs or PlanSaveAll. It holds the bytes to write and
// the disk state the file must still have. Run does the disk work and reads no editor
// state, so it may run in a command. ApplySaves records its result on the editor's
// goroutine.
type SaveJob struct {
	doc    *Document
	name   string    // the file name, for the changed-on-disk message
	path   string    // the file to write
	data   []byte    // the bytes to write, encoded and with the line endings applied
	text   string    // the buffer text that data encodes
	ends   []string  // the line endings written, when the file mixes them; see Document.ends
	label  string    // the encoding label of text, see encodingLabel
	expect diskState // the disk state of path when the job was planned
	// checkDisk makes Run refuse a file whose state differs from expect. Save and
	// auto-save check it. Save As does not, because it writes over the file it names.
	checkDisk bool
	// refuseExisting makes Run refuse an existing file other than ownPath. Save As
	// sets it when it is not forced to overwrite.
	refuseExisting bool
	ownPath        string // the document's own path, which Save As may write over
	err            error  // set when the text cannot be encoded; Run then writes nothing
	// large is the stream of a large document, which is written from a snapshot of its
	// buffer instead of from data. It is nil for any other document. See save_large.go.
	large *largeSave
}

// SaveResult is the outcome of running a SaveJob. ApplySaves records it.
type SaveResult struct {
	job  SaveJob
	disk diskState // the state of the file after the write, when it was written
	err  error

	// The rest is set for a large document only.
	res     buffer.WriteResult // what the snapshot wrote, for the rebase
	tmp     os.FileInfo        // the state of the temporary file before it was renamed
	skipped bool               // nothing was written, because there was nothing to save
	note    string             // what the status says after the save, or empty
}

// Err returns the error of the save, or nil when the file was written.
func (r SaveResult) Err() error { return r.err }

// PlanAutoSave prepares the saves that AutoSave would make at now, and records now
// as the time of the last auto-save. It changes no file. Each named, modified,
// writable document gets a job. The save-time options (trailing whitespace and the
// final newline) are applied to its buffer here, as a save applies them. Untitled
// and read-only documents get no job. With auto-save off it returns nil and records
// no time. The caller runs the jobs with Run, then passes the results to ApplySaves.
func (e *Editor) PlanAutoSave(now time.Time) []SaveJob {
	if e.opts.AutoSaveInterval <= 0 {
		return nil
	}
	e.autoSaved = now
	var jobs []SaveJob
	for _, d := range e.docs {
		if d.ext.virtual || d.Untitled() || d.readOnly || d.large != nil || !d.Modified() {
			continue
		}
		jobs = append(jobs, d.planSave(d.Path, e.opts))
	}
	return jobs
}

// planSave prepares the save of d to path: it applies the save-time options to the
// buffer and encodes the text. A text the codec cannot hold makes a job that fails.
// The job checks the disk state of path, as a save to the document's own file does.
func (d *Document) planSave(path string, o Options) SaveJob {
	job := SaveJob{doc: d, name: d.Name(), path: path, expect: d.disk, checkDisk: true}
	if d.binary {
		job.err = fmt.Errorf("save %s: %w", path, ErrBinary)
		return job
	}
	if d.large != nil {
		// A large document is saved by streaming its snapshot, so its text is never held
		// whole and is never tidied with the whole-text edits below.
		return d.planSaveLarge(path, o, job)
	}
	d.tidyForSave(o)
	job.text = d.Buffer.Text()
	job.label = encodingLabel(d.codecName(), job.text)
	if d.ends != nil {
		job.ends = d.breaks(job.text)
	}
	data, err := encodeText(d.codecName(), d.fileText(job.text))
	if err != nil {
		job.err = fmt.Errorf("save %s: %w", path, err)
		return job
	}
	job.data = data
	return job
}

// Run writes the job's bytes to its file, after it checks that the file still has
// the disk state of the plan. A file that changed since then is not written, and the
// result carries ErrChangedOnDisk. Run reads only the job, so it is safe to call on
// any goroutine. It is RunContext with a context that is never cancelled.
func (j SaveJob) Run() SaveResult {
	return j.RunContext(context.Background())
}

// RunContext is Run for a caller that may stop the write. The write of a large
// document checks ctx about every MiB, and a cancelled one removes its temporary file
// and returns the context's error. A document that is not large is written in one go
// and ignores ctx.
func (j SaveJob) RunContext(ctx context.Context) SaveResult {
	if j.large != nil {
		// The snapshot holds the file open. Every path out of Run lets go of it.
		defer j.large.snap.Release()
	}
	if j.err != nil {
		return SaveResult{job: j, err: j.err}
	}
	if j.refuseExisting && !sameFile(j.ownPath, j.path) && diskStateOf(j.path).exists {
		return SaveResult{job: j, err: fmt.Errorf("save as %s: %w", j.path, ErrExists)}
	}
	now := diskStateOf(j.path)
	if j.large != nil && j.large.noop && now.exists && now.sameAs(j.expect) {
		// Nothing was changed and the file is as the document left it.
		return SaveResult{job: j, disk: now, skipped: true}
	}
	if j.checkDisk && now.exists && !now.sameAs(j.expect) {
		return SaveResult{job: j, err: fmt.Errorf("%s %w", j.name, ErrChangedOnDisk)}
	}
	if j.large != nil {
		return j.writeLarge(ctx)
	}
	note, err := writeFileAtomic(j.path, j.data)
	if err != nil {
		return SaveResult{job: j, err: fmt.Errorf("save %s: %w", j.path, err)}
	}
	return SaveResult{job: j, disk: diskStateOf(j.path), note: saveNote("", note)}
}

// ApplySaves records the results of planned saves on the editor's goroutine. A
// written file marks its document saved with the text that was written and its disk
// state. The buffer is marked saved only when its text is still that text, so an
// edit made while the save ran keeps the document modified. A failed save keeps the
// document's edits and shows its error in the document's status. It returns the
// failures joined, or nil when all saves worked.
func (e *Editor) ApplySaves(results []SaveResult) error {
	var errs []error
	for _, r := range results {
		d := r.job.doc
		if r.err != nil {
			d.status = r.err.Error()
			errs = append(errs, r.err)
			continue
		}
		if r.job.large != nil {
			e.applyLargeSave(r)
			continue
		}
		d.setSaved(r.job.text)
		d.saveNote = r.note
		if d.ends != nil {
			d.ends = r.job.ends
		}
		d.encoding = r.job.label
		d.disk = r.disk
		if d.contentMark() == (contentMark{text: r.job.text}) {
			d.Buffer.MarkSaved()
			d.metaDirty = false
		}
	}
	return errors.Join(errs...)
}
