package app

import (
	"errors"
	"time"

	"github.com/tbxark/gvim/internal/editor"
)

// errAutoSaveWriting is what an explicit save returns while a planned auto-save is
// writing. The disk state of the files is recorded only when the write's result is
// applied, so a save before then would see its own write as a change by another
// program. The message is the status row text, so the user knows to try again.
var errAutoSaveWriting = errors.New("saving in progress, try again")

// autoSaveGate records that a planned auto-save is writing. The copies of the editor
// adapter share it, so it is a pointer. A nil gate never blocks.
type autoSaveGate struct {
	writing bool
}

// AutoSaveDue reports whether the editor's auto-save is due at now. The root's
// auto-save tick asks it once a second. These methods are on the adapter, so the
// root reaches the editor's auto-save through its EditorState.
func (a editorAdapter) AutoSaveDue(now time.Time) bool {
	return a.ed.AutoSaveDue(now)
}

// AutoSave saves the modified, named documents when a save is due. The error joins
// the failures of the documents that did not save. It runs the writes on the caller's
// goroutine, so the root uses PlanAutoSave, ApplyAutoSave and a command instead.
func (a editorAdapter) AutoSave(now time.Time) error {
	return a.ed.AutoSave(now)
}

// PlanAutoSave prepares the auto-save at now, as editor.PlanAutoSave does, and marks
// the gate as writing when there is a save to run. While a plan is writing, it plans
// nothing and records no time, so the next tick plans again.
func (a editorAdapter) PlanAutoSave(now time.Time) []editor.SaveJob {
	if a.saves != nil && a.saves.writing {
		return nil
	}
	jobs := a.ed.PlanAutoSave(now)
	if a.saves != nil && len(jobs) > 0 {
		a.saves.writing = true
	}
	return jobs
}

// ApplyAutoSave records the results of a planned auto-save on the editor's goroutine,
// and opens the gate again. The results come from the command that ran the jobs.
func (a editorAdapter) ApplyAutoSave(results []editor.SaveResult) error {
	if a.saves != nil {
		a.saves.writing = false
	}
	return a.ed.ApplySaves(results)
}
