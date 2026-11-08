package tui

import (
	"testing"

	"github.com/tbxark/gvim/editor"
)

// TestReadRevertMarksAutomaticReloads: the plan of a reload that the root starts because the
// file changed on disk says so, so the editor words its status "Reloaded", not "Reverted".
func TestReadRevertMarksAutomaticReloads(t *testing.T) {
	t.Parallel()
	for _, auto := range []bool{false, true} {
		ed := &revertEditor{docEditor: newDocEditor("/ws/a.txt", false)}
		readRevert(ed, editor.RevertPlan{Path: "/ws/a.txt"}, auto)()
		if ed.plan.Auto != auto {
			t.Errorf("auto %v: the plan read has Auto = %v", auto, ed.plan.Auto)
		}
	}
}
