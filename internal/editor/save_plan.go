package editor

import "fmt"

// SavePlan is an explicit save prepared on the editor's goroutine: Save, Save As or
// Save All. PlanSave, PlanSaveAs and PlanSaveAll build it and change no file. Run
// writes its files and reads only the plan, so it may run in a command. ApplySavePlan
// records the results on the editor's goroutine. Save, Save As and Save All are each a
// plan, a run and an apply in one call, so the synchronous and the asynchronous saves
// write the same way.
type SavePlan struct {
	jobs     []SaveJob
	all      bool   // Save All: the status is the summary of the tabs
	untitled int    // Save All: modified tabs with no file name, skipped
	readOnly int    // Save All: modified read-only tabs, skipped
	renameTo string // Save As: the path the document takes once it is written
}

// Run writes the planned files one after another and returns their results. It reads
// only the plan, so it may run in a command.
func (p SavePlan) Run() []SaveResult {
	results := make([]SaveResult, len(p.jobs))
	for i, j := range p.jobs {
		results[i] = j.Run()
	}
	return results
}

// PlanSave prepares the save of the active document, as Save does, and changes no
// file. A missing tab, a read-only document and an untitled one are refused with
// their errors, which are also the tab's status. The save-time options are applied to
// the buffer here.
func (e *Editor) PlanSave() (SavePlan, error) {
	d := e.Active()
	if d == nil {
		err := ErrNoDocument
		e.setStatus(err.Error())
		return SavePlan{}, err
	}
	if d.readOnly {
		err := ErrReadOnly
		e.setStatus(err.Error())
		return SavePlan{}, err
	}
	if d.Untitled() {
		err := ErrNoPath
		e.setStatus(err.Error())
		return SavePlan{}, err
	}
	return SavePlan{jobs: []SaveJob{d.planSave(d.Path, e.opts)}}, nil
}

// PlanSaveAs prepares the write of the active document to path, as SaveAs does, and
// changes no file. It refuses a path that another tab has open, and an empty path. An
// existing file other than the document's own is refused when the write runs, unless
// force is set. The document takes path once the write works (see ApplySavePlan).
func (e *Editor) PlanSaveAs(path string, force bool) (SavePlan, error) {
	d := e.Active()
	if d == nil {
		err := fmt.Errorf("save as: %w", ErrNoDocument)
		e.setStatus(err.Error())
		return SavePlan{}, err
	}
	path, err := absPath(path)
	if err != nil {
		e.setStatus(err.Error())
		return SavePlan{}, err
	}
	if i := e.openElsewhere(path, e.active); i >= 0 {
		err := fmt.Errorf("save as %s: %w", path, ErrAlreadyOpen)
		e.setStatus(err.Error())
		return SavePlan{}, err
	}
	if path == "" {
		err := fmt.Errorf("save as: %w", ErrEmptyPath)
		e.setStatus(err.Error())
		return SavePlan{}, err
	}
	job := d.planSave(path, e.opts)
	job.checkDisk = false
	job.refuseExisting = !force
	job.ownPath = d.Path
	return SavePlan{jobs: []SaveJob{job}, renameTo: path}, nil
}

// PlanSaveAll prepares the save of every modified tab, as save_all does, and changes
// no file. A modified tab with no file name, or a read-only one, gets no job. It is
// counted instead, for the summary.
func (e *Editor) PlanSaveAll() (SavePlan, error) {
	if e.Active() == nil {
		err := ErrNoDocument
		e.setStatus(err.Error())
		return SavePlan{}, err
	}
	p := SavePlan{all: true}
	for _, d := range e.docs {
		if !d.Modified() {
			continue
		}
		switch {
		case d.readOnly:
			p.readOnly++
		case d.Untitled():
			p.untitled++
		default:
			p.jobs = append(p.jobs, d.planSave(d.Path, e.opts))
		}
	}
	return p, nil
}

// ApplySavePlan records the results of a planned save on the editor's goroutine. A
// written file marks its document saved (see ApplySaves), and a Save As then takes
// its path and becomes a permanent tab. The document that was saved gets the status
// "Saved", or the failure. Save All sets the active tab's status to its summary. It
// returns the failure, or nil when every write worked.
func (e *Editor) ApplySavePlan(p SavePlan, results []SaveResult) error {
	err := e.ApplySaves(results)
	if p.all {
		return e.saveAllStatus(results, p)
	}
	if len(results) == 0 || err != nil {
		return err
	}
	d := results[0].job.doc
	if p.renameTo != "" {
		d.rename(p.renameTo)
		d.preview = false
	}
	d.status = "Saved"
	return nil
}
