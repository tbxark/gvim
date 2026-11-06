package ui

import (
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/editor"
)

// asyncSaver is the editor's explicit-save surface, as the app adapter provides it. A
// save is planned on the update loop, written in a command and applied in the message,
// as an auto-save is. WriteSave is the write itself, so the command's write goes through
// the port. An editor without the surface saves through Do.
type asyncSaver interface {
	PlanSave() (editor.SavePlan, error)
	PlanSaveAs(path string, force bool) (editor.SavePlan, error)
	PlanSaveAll() (editor.SavePlan, error)
	WriteSave(p editor.SavePlan) []editor.SaveResult
	ApplySavePlan(p editor.SavePlan, results []editor.SaveResult) error
}

// saveDoneMsg is the outcome of the writes of an explicit save. The results are applied
// on the update loop, and then the flow goes on. then is the unsaved-changes flow that
// waits for the save, and reload says the explorer reloads after a Save As.
type saveDoneMsg struct {
	plan    editor.SavePlan
	results []editor.SaveResult
	then    flow
	reload  bool
}

// save runs Ctrl+S. An untitled buffer has no path, so the Save As prompt opens instead.
func (m *Model) save() tea.Cmd {
	return m.saveThen(flow{})
}

// saveThen runs the save of the active tab, and then goes on with f. The save is planned
// here and written by the command it returns, so the file is not written in Update.
func (m *Model) saveThen(f flow) tea.Cmd {
	if as, ok := m.ed.(asyncSaver); ok {
		p, err := as.PlanSave()
		return m.planned(as, p, err, f, false)
	}
	return m.finished(m.ed.Do("save", nil).Err, f, false)
}

// saveAs runs Save As to path. The editor refuses a path that another tab has open, and
// an existing file when force is false. The Save As dialog asks about an existing file
// before it sends Replace, so the refusal only shows up when the dialog could not check
// the disk. A success reloads the explorer and goes on with the flow.
func (m *Model) saveAs(path string, force bool, f flow) tea.Cmd {
	if as, ok := m.ed.(asyncSaver); ok {
		p, err := as.PlanSaveAs(path, force)
		return m.planned(as, p, err, f, true)
	}
	args := map[string]any{"path": path, "force": force}
	return m.finished(m.ed.Do("save_as", args).Err, f, true)
}

// saveAll runs save_all. Its summary is the status of the active tab, which the editor
// sets when the results are applied.
func (m *Model) saveAll() tea.Cmd {
	if as, ok := m.ed.(asyncSaver); ok {
		p, err := as.PlanSaveAll()
		return m.planned(as, p, err, flow{}, false)
	}
	return m.finished(m.ed.Do("save_all", nil).Err, flow{}, false)
}

// planned starts the writes of a planned save. A plan that was refused goes to finished,
// which shows the refusal. Otherwise the command writes the files, and its result comes
// back as a saveDoneMsg. The command reads only the plan, so it writes off the loop.
func (m *Model) planned(as asyncSaver, p editor.SavePlan, err error, f flow, reload bool) tea.Cmd {
	if err != nil {
		return m.finished(err, f, reload)
	}
	return func() tea.Msg {
		return saveDoneMsg{plan: p, results: as.WriteSave(p), then: f, reload: reload}
	}
}

// saveDone applies the results of a save's writes on the update loop, and then goes on
// with the flow the save was started for.
func (m *Model) saveDone(msg saveDoneMsg) tea.Cmd {
	as, ok := m.ed.(asyncSaver)
	if !ok {
		return nil
	}
	return m.finished(as.ApplySavePlan(msg.plan, msg.results), msg.then, msg.reload)
}

// finished goes on with the flow of an explicit save, once the editor has answered. A
// save of an untitled buffer opens the Save As prompt. Any other failure goes to the
// notice row and stops the flow, because the editor's own status is not always shown.
// A saved Save As reloads the explorer when reload is set, and the flow goes on.
func (m *Model) finished(err error, f flow, reload bool) tea.Cmd {
	if errors.Is(err, editor.ErrNoPath) {
		m.askSaveAs(f)
		return nil
	}
	if err != nil {
		m.notice = err.Error()
		return nil
	}
	var cmd tea.Cmd
	if reload {
		cmd = m.reloadTreeAsync()
	}
	return tea.Batch(cmd, m.afterSave(f))
}
