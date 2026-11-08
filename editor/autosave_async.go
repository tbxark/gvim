package editor

import (
	"errors"
	"time"
)

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
		if d.Ext().Virtual || d.Untitled() || d.ReadOnly() || d.LargeState() != nil || !d.Modified() {
			continue
		}
		jobs = append(jobs, d.PlanSave(d.Path, e.opts.documentSettings()))
	}
	return jobs
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
		d := r.Job.Doc
		if r.Err() != nil {
			d.Fail(r.Err())
			errs = append(errs, r.Err())
			continue
		}
		if r.Job.LargeSave() != nil {
			e.applyLargeSave(r)
			continue
		}
		d.SetSaved(r.Job.Text)
		d.SaveNote = r.Note
		if d.Ends != nil {
			d.Ends = r.Job.Ends
		}
		d.SetEncodingLabel(r.Job.Label)
		d.Disk = r.Disk
		if d.ContentMark() == (contentMark{Text: r.Job.Text}) {
			d.Buffer.MarkSaved()
			d.MetaDirty = false
		}
	}
	return errors.Join(errs...)
}
