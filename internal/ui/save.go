package ui

import (
	"context"
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
// waits for the save, and reload says the explorer reloads after a Save As. ask is set
// for a Save of the active document, whose refusal over a changed file asks to overwrite.
type saveDoneMsg struct {
	plan    editor.SavePlan
	results []editor.SaveResult
	then    flow
	reload  bool
	ask     bool
	// id numbers the write among those in flight, so that a quit can stop it and wait
	// for it. Zero is a write that nothing tracks.
	id int
}

// save runs Ctrl+S. An untitled buffer has no path, so the Save As prompt opens instead.
func (m *Model) save() tea.Cmd {
	return m.saveThen(flow{})
}

// saveThen runs the save of the active tab, and then goes on with f. The save is planned
// here and written by the command it returns, so the file is not written in Update. The
// plugins may cancel it first (before_file_save), which ends the flow as well. A buffer
// without a path (the Save As dialog follows and raises the event for its target) and a
// read-only one (the editor refuses) are not gated, because nothing is written for them.
func (m *Model) saveThen(f flow) tea.Cmd {
	if !m.activeSaveWrites() {
		cmd, _ := m.saveThenErr(f)
		return cmd
	}
	cancel, _, gate := m.pluginSaveGate("manual")
	if cancel {
		return gate
	}
	cmd, _ := m.saveThenErr(f)
	return tea.Batch(gate, cmd)
}

// activeSaveWrites reports whether a save of the active buffer can write a file: it has a
// path and is writable. Without a plugin editor port it says yes, as the gate then has
// nothing to name.
func (m *Model) activeSaveWrites() bool {
	pe := m.plug.pe
	if pe == nil {
		return true
	}
	info, ok := pe.Buffer(pe.ActiveBuffer())
	return !ok || (info.Path != "" && !info.ReadOnly)
}

// saveThenErr is saveThen that also returns the error of the plan or of the editor's
// save, which the plugin host reports instead of guessing it from the notice row.
func (m *Model) saveThenErr(f flow) (tea.Cmd, error) {
	if as, ok := m.ed.(asyncSaver); ok {
		p, err := as.PlanSave()
		return m.planned(as, p, err, f, false, true), err
	}
	err := m.ed.Do("save", nil).Err
	return m.finished(err, f, false), err
}

// saveAs runs Save As to path. The editor refuses a path that another tab has open, and
// an existing file when force is false. The Save As dialog asks about an existing file
// before it sends Replace, so the refusal only shows up when the dialog could not check
// the disk. A success reloads the explorer and goes on with the flow.
func (m *Model) saveAs(path string, force bool, f flow) tea.Cmd {
	cancel, gate := m.pluginSaveAsGate(path)
	if cancel {
		return gate
	}
	cmd, _ := m.saveAsErr(path, force, f)
	return tea.Batch(gate, cmd)
}

// saveAsErr is saveAs that also returns the error of the plan or of the editor's save.
func (m *Model) saveAsErr(path string, force bool, f flow) (tea.Cmd, error) {
	if as, ok := m.ed.(asyncSaver); ok {
		p, err := as.PlanSaveAs(path, force)
		return m.planned(as, p, err, f, true, false), err
	}
	args := map[string]any{"path": path, "force": force}
	err := m.ed.Do("save_as", args).Err
	return m.finished(err, f, true), err
}

// saveAll runs save_all. Its summary is the status of the active tab, which the editor
// sets when the results are applied. The plugins may cancel it first (before_file_save).
func (m *Model) saveAll() tea.Cmd {
	cancel, gate := m.pluginSaveAllGate("manual")
	if cancel {
		return gate
	}
	return tea.Batch(gate, m.saveAllPlanned())
}

// saveAllPlanned plans and starts the writes of save_all.
func (m *Model) saveAllPlanned() tea.Cmd {
	if as, ok := m.ed.(asyncSaver); ok {
		p, err := as.PlanSaveAll()
		return m.plannedFor(true, as, p, err, flow{}, false, false)
	}
	return m.finished(m.ed.Do("save_all", nil).Err, flow{}, false)
}

// planned starts the writes of a planned save. A plan that was refused goes to finished,
// which shows the refusal. Otherwise the command writes the files, and its result comes
// back as a saveDoneMsg. The command reads only the plan, so it writes off the loop.
func (m *Model) planned(as asyncSaver, p editor.SavePlan, err error, f flow, reload, ask bool) tea.Cmd {
	return m.plannedFor(false, as, p, err, f, reload, ask)
}

// plannedFor is planned, with all set when the plan saves every modified tab.
func (m *Model) plannedFor(all bool, as asyncSaver, p editor.SavePlan, err error, f flow, reload, ask bool) tea.Cmd {
	if err != nil {
		return m.finished(err, f, reload)
	}
	// The write of a large document takes long: it can be stopped by a quit, and the
	// notice row says it is running.
	id, ctx := m.beginSave(all)
	m.savingStatus()
	return func() tea.Msg {
		return saveDoneMsg{plan: p, results: writeSave(ctx, as, p), then: f, reload: reload, ask: ask, id: id}
	}
}

// saveDone applies the results of a save's writes on the update loop, and then goes on
// with the flow the save was started for.
func (m *Model) saveDone(msg saveDoneMsg) tea.Cmd {
	m.endSave(msg.id)
	m.clearSaving()
	as, ok := m.ed.(asyncSaver)
	if !ok {
		return m.quitIfWaiting()
	}
	err := as.ApplySavePlan(msg.plan, msg.results)
	m.plugSaved(msg.results)
	if cmd := m.quitIfWaiting(); cmd != nil {
		// A quit was waiting for this write. Its results are recorded, and the program ends.
		return cmd
	}
	if errors.Is(err, context.Canceled) {
		// The write was stopped, because its tab closed. Nothing is wrong with the file.
		m.notice = "Save cancelled"
		return nil
	}
	if msg.ask && errors.Is(err, editor.ErrChangedOnDisk) && m.askOverwriteSave(err, msg.then) {
		return nil
	}
	return m.finished(err, msg.then, msg.reload)
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
