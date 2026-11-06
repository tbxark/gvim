package app

import (
	"time"

	"github.com/tbxark/gvim/internal/config"
	"github.com/tbxark/gvim/internal/editor"
)

// PlanSaveForce prepares the save of the active document over a file that changed on
// disk. It is refused and gated as PlanSave is.
func (a editorAdapter) PlanSaveForce() (editor.SavePlan, error) {
	if a.writePending() {
		return editor.SavePlan{}, errAutoSaveWriting
	}
	p, err := a.ed.PlanSaveForce()
	a.markWriting(err)
	return p, err
}

// autoRevertPoll is how often the root checks the open files for outside changes:
// editor.auto_revert_poll_interval_ms.
func autoRevertPoll(cfg config.Config) time.Duration {
	return time.Duration(cfg.AutoRevertPollIntervalMS) * time.Millisecond
}

// PlanDiskCheck records the disk state of the open files for the auto-revert poll, as
// the editor's PlanDiskCheck does.
func (a editorAdapter) PlanDiskCheck() func() []editor.DiskChange {
	return a.ed.PlanDiskCheck()
}
