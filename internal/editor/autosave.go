package editor

import "time"

// AutoSaveDue reports whether AutoSave should run at now. Auto-save is on when
// Options.AutoSaveInterval is positive, and then it is due once that interval has
// passed since the last AutoSave. The first AutoSave is due at once. With auto-save
// off it is never due. The caller checks this on a timer, then calls AutoSave.
func (e *Editor) AutoSaveDue(now time.Time) bool {
	if e.opts.AutoSaveInterval <= 0 {
		return false
	}
	if e.autoSaved.IsZero() {
		return true
	}
	return now.Sub(e.autoSaved) >= e.opts.AutoSaveInterval
}

// AutoSave saves every named, modified document that is not read-only, and records
// now as the time of the last AutoSave. Untitled documents are skipped, because
// they have no file, and so are read-only ones. A document whose save fails keeps
// its edits, and its status shows the error. The save of the other documents goes
// on. The returned error joins every failure, and is nil when all saves worked.
// With auto-save off, AutoSave does nothing and records no time.
func (e *Editor) AutoSave(now time.Time) error {
	jobs := e.PlanAutoSave(now)
	results := make([]SaveResult, len(jobs))
	for i, j := range jobs {
		results[i] = j.Run()
	}
	return e.ApplySaves(results)
}
