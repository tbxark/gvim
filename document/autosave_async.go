package document

import (
	"context"
	"fmt"
	"github.com/tbxark/gvim/atomicfile"
	"github.com/tbxark/gvim/buffer"
	"os"
)

// SaveJob is one file write of a save, prepared on the editor's goroutine by
// PlanAutoSave, PlanSave, PlanSaveAs or PlanSaveAll. It holds the bytes to write and
// the disk state the file must still have. Run does the disk work and reads no editor
// state, so it may run in a command. ApplySaves records its result on the editor's
// goroutine.
type SaveJob struct {
	Doc    *Document
	Name   string    // the file name, for the changed-on-disk message
	Path   string    // the file to write
	Data   []byte    // the bytes to write, encoded and with the line endings applied
	Text   string    // the buffer text that data encodes
	Ends   []string  // the line endings written, when the file mixes them; see Document.ends
	Label  string    // the encoding label of text, see encodingLabel
	Expect diskState // the disk state of path when the job was planned
	// checkDisk makes Run refuse a file whose state differs from expect. Save and
	// auto-save check it. Save As does not, because it writes over the file it names.
	CheckDisk bool
	// refuseExisting makes Run refuse an existing file other than ownPath. Save As
	// sets it when it is not forced to overwrite.
	RefuseExisting bool
	OwnPath        string // the document's own path, which Save As may write over
	err            error  // set when the text cannot be encoded; Run then writes nothing
	// large is the stream of a large document, which is written from a snapshot of its
	// buffer instead of from data. It is nil for any other document. See save_large.go.
	large *largeSave
}

// SaveResult is the outcome of running a SaveJob. ApplySaves records it.
type SaveResult struct {
	Job  SaveJob
	Disk diskState // the state of the file after the write, when it was written
	err  error

	// The rest is set for a large document only.
	Res     buffer.WriteResult // what the snapshot wrote, for the rebase
	Tmp     os.FileInfo        // the state of the temporary file before it was renamed
	Skipped bool               // nothing was written, because there was nothing to save
	Note    string             // what the status says after the save, or empty
}

// Err returns the error of the save, or nil when the file was written.
func (r SaveResult) Err() error { return r.err }

// planSave prepares the save of d to path: it applies the save-time options to the
// buffer and encodes the text. A text the codec cannot hold makes a job that fails.
// The job checks the disk state of path, as a save to the document's own file does.
func (d *Document) PlanSave(path string, o Settings) SaveJob {
	job := SaveJob{Doc: d, Name: d.Name(), Path: path, Expect: d.Disk, CheckDisk: true}
	if d.binary {
		job.err = fmt.Errorf("save %s: %w", path, ErrBinary)
		return job
	}
	if d.large != nil {
		// A large document is saved by streaming its snapshot, so its text is never held
		// whole and is never tidied with the whole-text edits below.
		return d.planSaveLarge(path, o, job)
	}
	d.TidyForSave(o)
	job.Text = d.Buffer.Text()
	job.Label = encodingLabel(d.CodecName(), job.Text)
	if d.Ends != nil {
		job.Ends = d.breaks(job.Text)
	}
	data, err := encodeText(d.CodecName(), d.fileText(job.Text))
	if err != nil {
		job.err = fmt.Errorf("save %s: %w", path, err)
		return job
	}
	job.Data = data
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
		defer j.large.Snap.Release()
	}
	if j.err != nil {
		return SaveResult{Job: j, err: j.err}
	}
	if j.RefuseExisting && !sameFile(j.OwnPath, j.Path) && diskStateOf(j.Path).Exists {
		return SaveResult{Job: j, err: fmt.Errorf("save as %s: %w", j.Path, ErrExists)}
	}
	now := diskStateOf(j.Path)
	if j.large != nil && j.large.Noop && now.Exists && now.SameAs(j.Expect) {
		// Nothing was changed and the file is as the document left it.
		return SaveResult{Job: j, Disk: now, Skipped: true}
	}
	if j.CheckDisk && now.Exists && !now.SameAs(j.Expect) {
		return SaveResult{Job: j, err: fmt.Errorf("%s %w", j.Name, ErrChangedOnDisk)}
	}
	if j.large != nil {
		return j.writeLarge(ctx)
	}
	note, err := atomicfile.WriteFile(j.Path, j.Data)
	if err != nil {
		return SaveResult{Job: j, err: fmt.Errorf("save %s: %w", j.Path, err)}
	}
	return SaveResult{Job: j, Disk: diskStateOf(j.Path), Note: saveNote("", note)}
}
