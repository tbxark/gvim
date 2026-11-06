package ui

import (
	"errors"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/ui/prompt"
)

// asyncReverter is the editor's revert surface, as the app adapter provides it. A revert
// is planned on the update loop, read in a command and applied in the message, as a save
// is. RunRevert reads only the plan, and ApplyRevert finds the document by the plan's path.
// An editor without the surface reverts through Do, which reads the file on the update loop.
type asyncReverter interface {
	PlanRevert(force bool) (editor.RevertPlan, error)
	RunRevert(plan editor.RevertPlan) editor.RevertResult
	ApplyRevert(plan editor.RevertPlan, res editor.RevertResult) error
}

// revertTag is the tag of the Discard changes? confirm that Revert File opens.
type revertTag struct{}

// revertRetryTag is the tag of the confirm that asks again when the text changed while the
// file was read.
type revertRetryTag struct{}

// revertDoneMsg is the file that a revert read, with the plan it was read for. It is applied
// on the update loop. auto is set when auto-revert started the reload, which asks nothing.
type revertDoneMsg struct {
	plan   editor.RevertPlan
	result editor.RevertResult
	auto   bool
}

// revertFile runs Revert File. A buffer with no file has nothing to revert to, and a
// modified one asks before its changes are discarded, unless force is set.
func (m *Model) revertFile(force bool) tea.Cmd {
	doc := m.ed.Active()
	if doc == nil {
		m.notice = noDocumentNotice
		return nil
	}
	if docPath(doc) == "" {
		m.notice = editor.ErrNoPath.Error()
		return nil
	}
	if doc.Modified() && !force {
		m.pushOverlay(confirmOverlay{c: prompt.NewConfirm(m.styles, "Discard changes?", revertTag{})})
		return nil
	}
	return m.startRevert(force)
}

// revertAnswer runs the revert that the Discard changes? confirm answered Yes to.
func (m *Model) revertAnswer(d prompt.Done) tea.Cmd {
	if d.Outcome != prompt.Yes {
		return nil
	}
	return m.startRevert(true)
}

// startRevert plans the revert of the active buffer and returns the command that reads
// the file. The plan's refusal is the notice. An editor without the revert surface runs
// the revert action, and the editor's refusal is the notice.
func (m *Model) startRevert(force bool) tea.Cmd {
	as, ok := m.ed.(asyncReverter)
	if !ok {
		res := m.editorDo("revert", map[string]any{"force": force})
		m.syncViewport()
		if res.Err != nil {
			m.notice = res.Err.Error()
		}
		return nil
	}
	plan, err := as.PlanRevert(force)
	if err != nil {
		m.notice = err.Error()
		return nil
	}
	return readRevert(as, plan, false)
}

// readRevert returns the command that reads the file of plan. The read runs in the command,
// and the result is applied when its message reaches Update.
func readRevert(as asyncReverter, plan editor.RevertPlan, auto bool) tea.Cmd {
	return func() tea.Msg {
		return revertDoneMsg{plan: plan, result: as.RunRevert(plan), auto: auto}
	}
}

// revertDone applies the file that a revert read. A read that failed is the notice, and
// the buffer keeps its text. A text that changed after the plan asks again, since the file
// read may no longer match the buffer. An auto-revert does not ask: its notice says why.
func (m *Model) revertDone(msg revertDoneMsg) tea.Cmd {
	as, ok := m.ed.(asyncReverter)
	if !ok {
		return nil
	}
	if msg.result.Err != nil {
		m.notice = msg.result.Err.Error()
		return nil
	}
	err := as.ApplyRevert(msg.plan, msg.result)
	m.syncViewport()
	switch {
	case errors.Is(err, editor.ErrStaleRevert) && !msg.auto:
		m.pushOverlay(confirmOverlay{c: prompt.NewConfirm(m.styles, "Text changed during the revert. Try again?", revertRetryTag{})})
	case err != nil:
		m.notice = err.Error()
	}
	return nil
}

// revertRetryAnswer runs the revert again when the retry confirm answered Yes. It goes
// through revertFile, so a buffer the user has changed since asks before it discards.
func (m *Model) revertRetryAnswer(d prompt.Done) tea.Cmd {
	if d.Outcome != prompt.Yes {
		return nil
	}
	return m.revertFile(false)
}

// toggleAutoRevert turns auto-revert on or off for the active buffer's file. The flag
// follows the path, so it stays with the file while the file is open.
func (m *Model) toggleAutoRevert() tea.Cmd {
	doc := m.ed.Active()
	if doc == nil {
		m.notice = noDocumentNotice
		return nil
	}
	p := docPath(doc)
	if p == "" {
		m.notice = editor.ErrNoPath.Error()
		return nil
	}
	name := filepath.Base(p)
	if m.fix.autoRevert[p] {
		delete(m.fix.autoRevert, p)
		m.notice = "Auto-revert off for " + name
		return nil
	}
	if m.fix.autoRevert == nil {
		m.fix.autoRevert = map[string]bool{}
	}
	m.fix.autoRevert[p] = true
	m.notice = "Auto-revert on for " + name
	return nil
}

// tabFile is an open document that has a file, as auto-revert sees it.
type tabFile struct {
	path     string
	modified bool
	active   bool
}

// openTabFiles returns the open documents that have a file: the active one first, then the
// background tabs in tab order. An untitled tab has no file and is left out.
func (m *Model) openTabFiles() []tabFile {
	var out []tabFile
	if doc := m.ed.Active(); doc != nil {
		if p := docPath(doc); p != "" {
			out = append(out, tabFile{path: p, modified: doc.Modified(), active: true})
		}
	}
	for _, t := range m.ed.Tabs() {
		if !t.Active && t.Path != "" {
			out = append(out, tabFile{path: t.Path, modified: t.Modified})
		}
	}
	return out
}

// autoRevertChanged reloads each flagged document whose own file the watcher reports as
// changed, whether it is the active tab or a background tab. A folder that changed without
// its file reloads nothing. A document with unsaved edits keeps its text, and the notice says
// so. It returns the commands that read the files.
func (m *Model) autoRevertChanged(files []string) tea.Cmd {
	if len(files) == 0 || len(m.fix.autoRevert) == 0 {
		return nil
	}
	changed := make(map[string]bool, len(files))
	for _, f := range files {
		changed[filepath.Clean(f)] = true
	}
	var cmds []tea.Cmd
	for _, f := range m.openTabFiles() {
		if !m.fix.autoRevert[f.path] || !changed[filepath.Clean(f.path)] {
			continue
		}
		if f.modified {
			m.notice = filepath.Base(f.path) + " changed on disk; unsaved changes kept"
			continue
		}
		cmds = append(cmds, m.autoReload(f))
	}
	return tea.Batch(cmds...)
}

// autoReload returns the command that reads the file of a clean flagged document. The
// active tab is planned by the editor's PlanRevert, as Revert File plans it. PlanRevert plans
// only the active tab, so a background tab's plan is built from its path, and its apply finds
// the tab by that path. An editor without the revert split reverts its active tab through the
// revert action.
func (m *Model) autoReload(f tabFile) tea.Cmd {
	as, ok := m.ed.(asyncReverter)
	switch {
	case f.active && !ok:
		return m.startRevert(false)
	case !ok:
		return nil
	case f.active:
		plan, err := as.PlanRevert(false)
		if err != nil {
			m.notice = err.Error()
			return nil
		}
		return readRevert(as, plan, true)
	}
	return readRevert(as, editor.RevertPlan{Path: f.path}, true)
}
