package ui

import (
	"testing"

	"github.com/tbxark/gvim/internal/editor"
)

// pathPlanEditor is an auto-reload editor that plans the reload of a background tab itself.
type pathPlanEditor struct {
	*autoTabsEditor
	planned []string // the paths PlanRevertPath was asked for
	plans   []editor.RevertPlan
}

func (e *pathPlanEditor) PlanRevertPath(path string) editor.RevertPlan {
	e.planned = append(e.planned, path)
	return editor.RevertPlan{Path: path}
}

func (e *pathPlanEditor) RunRevert(plan editor.RevertPlan) editor.RevertResult {
	e.plans = append(e.plans, plan)
	return e.autoTabsEditor.RunRevert(plan)
}

// TestAutoReloadOfABackgroundTabUsesTheEditorsPlan checks that a tab that is not active is
// planned by the editor when it can, so a large file is reopened as a large file, and that
// the active tab keeps going through PlanRevert.
func TestAutoReloadOfABackgroundTabUsesTheEditorsPlan(t *testing.T) {
	t.Parallel()
	ed := &pathPlanEditor{autoTabsEditor: newAutoTabsEditor(0, openDoc("/ws/a.txt", false), openDoc("/ws/b.txt", false))}
	m := newRevertModel(t, ed)
	_, cmd := m.Update(watchMsg{files: []string{"/ws/a.txt", "/ws/b.txt"}})
	feedMessages(m, cmd)
	if len(ed.planned) != 1 || ed.planned[0] != "/ws/b.txt" {
		t.Fatalf("PlanRevertPath calls = %q, want only the background tab", ed.planned)
	}
	if len(ed.plans) != 2 || ed.plans[0].Path != "/ws/a.txt" || ed.plans[1].Path != "/ws/b.txt" {
		t.Fatalf("plans = %+v", ed.plans)
	}
}
