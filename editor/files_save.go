package editor

// PlanSaveForce prepares the save of the active document as PlanSave does, but the
// write does not refuse a file that changed on disk since the document was opened or
// last saved. It is the Overwrite answer to ErrChangedOnDisk.
func (e *Editor) PlanSaveForce() (SavePlan, error) {
	p, err := e.PlanSave()
	if err != nil {
		return p, err
	}
	p.jobs[0].CheckDisk = false
	return p, nil
}
