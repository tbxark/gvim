package uiadapter

import (
	"github.com/tbxark/gvim/editor"
)

// PlanSaveForce prepares the save of the active document over a file that changed on
// disk. It is refused and gated as PlanSave is.
func (a *Editor) PlanSaveForce() (editor.SavePlan, error) {
	if a.writePending() {
		return editor.SavePlan{}, errAutoSaveWriting
	}
	p, err := a.ed.PlanSaveForce()
	a.markWriting(err)
	return p, err
}

// PlanDiskCheck records the disk state of the open files for the auto-revert poll, as
// the editor's PlanDiskCheck does.
func (a *Editor) PlanDiskCheck() func() []editor.DiskChange {
	return a.ed.PlanDiskCheck()
}
