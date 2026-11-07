package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/editor"
)

// autoSaveTickMsg is the once-a-second tick of auto-save. A tick asks the editor
// whether a save is due. When one is, the tick plans it and returns the write as a
// command. Otherwise it arms the next tick.
type autoSaveTickMsg struct{}

// autoSaveDoneMsg is the outcome of one auto-save write. Its results are applied on
// the update loop, which then arms the next tick.
type autoSaveDoneMsg struct {
	results []editor.SaveResult
}

// asyncAutoSaver is the editor's auto-save surface, as the app adapter provides it.
// Planning and applying run on the update loop, and the writes between them run in a
// command. The app adapter implements it, and the root asserts it on the EditorState.
type asyncAutoSaver interface {
	AutoSaveDue(now time.Time) bool
	PlanAutoSave(now time.Time) []editor.SaveJob
	ApplyAutoSave(results []editor.SaveResult) error
}

// autoSaveEvery is the period of the auto-save tick.
const autoSaveEvery = time.Second

// autoSaveCmd arms the next auto-save tick. It is nil when auto-save is off.
func (m *Model) autoSaveCmd() tea.Cmd {
	if m.root.ports.AutoSave <= 0 {
		return nil
	}
	return tea.Tick(autoSaveEvery, func(time.Time) tea.Msg { return autoSaveTickMsg{} })
}

// autoSaveTick plans a due save and returns the command that writes it. When no save
// is due, or the editor has no auto-save port, it arms the next tick instead. The
// write and the next tick never run together: the next tick is armed when the write's
// result is applied, so there is one chain of ticks.
func (m *Model) autoSaveTick() tea.Cmd {
	as, ok := m.ed.(asyncAutoSaver)
	if !ok {
		return m.autoSaveCmd()
	}
	now := m.now()
	if !as.AutoSaveDue(now) {
		return m.autoSaveCmd()
	}
	cancel, gate := m.pluginAutoSaveGate()
	if cancel {
		return tea.Batch(gate, m.autoSaveCmd())
	}
	jobs := as.PlanAutoSave(now)
	if len(jobs) == 0 {
		return tea.Batch(gate, m.autoSaveCmd())
	}
	return tea.Batch(gate, autoSaveWrite(jobs))
}

// autoSaveWrite returns the command that runs the planned saves. It reads no editor
// state, so it does its disk work off the update loop.
func autoSaveWrite(jobs []editor.SaveJob) tea.Cmd {
	return func() tea.Msg {
		results := make([]editor.SaveResult, len(jobs))
		for i, j := range jobs {
			results[i] = j.Run()
		}
		return autoSaveDoneMsg{results: results}
	}
}

// autoSaveDone applies the result of a write on the update loop, so the editor's
// documents record what was written. A failure goes to the notice row. The next tick
// is armed here.
func (m *Model) autoSaveDone(msg autoSaveDoneMsg) tea.Cmd {
	if as, ok := m.ed.(asyncAutoSaver); ok {
		if err := as.ApplyAutoSave(msg.results); err != nil {
			m.notice = "auto-save failed: " + err.Error()
		}
		m.plugSaved(msg.results)
	}
	return m.autoSaveCmd()
}
