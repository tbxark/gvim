// The unsaved-changes flow shared by closing a tab and quitting.

package tui

import (
	"fmt"
	"path/filepath"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/dialog"
)

// next says what a file action does once a save or a discard is done. The
// unsaved-changes flow of a close, and of a quit, use it.
type next int

const (
	nextNone  next = iota // nothing more: a Save As from Ctrl+S
	nextClose             // close the tab at index
	nextQuit              // run the quit loop again
)

// flow is the state of an unsaved-changes flow: what follows it, and the tab it
// is about. The tags of the dialogs carry it back to the root.
type flow struct {
	then  next
	index int
}

// The tags the file dialogs carry back. Each one holds the flow it belongs to.
type (
	unsavedTag struct{ f flow }
	// quitConfirmTag is the tag of the Quit/Cancel dialog of editor.confirm_quit.
	quitConfirmTag struct{}
	saveAsTag      struct{ f flow }
)

// requestClose closes the tab at index. A clean tab closes at once. A modified
// tab is activated and the unsaved-changes dialog asks about it first.
func (m *Model) requestClose(index int) tea.Cmd {
	t := m.tabPort()
	if t == nil {
		m.notice = "Closing tabs is not supported in this port"
		return nil
	}
	needs, err := t.CloseNeedsConfirm(index)
	if err != nil {
		m.notice = err.Error()
		return nil
	}
	if !needs {
		if err := m.closeTab(t, index, false); err != nil {
			m.notice = err.Error()
		}
		m.syncViewport()
		return nil
	}
	t.ActivateTab(index)
	m.syncViewport()
	m.askUnsaved(flow{then: nextClose, index: index}, m.ed.Tabs()[index].Name)
	return nil
}

// quitLoop is the one gate every quit goes through: it asks once about all modified tabs,
// including those retained by hot exit, and, when none is modified, asks for the
// confirmation editor.confirm_quit wants. Only a confirmed or forced quit reaches quit.
func (m *Model) quitLoop() tea.Cmd {
	if m.tabPort() == nil {
		return m.quitConfirmed()
	}
	count := 0
	if p, ok := m.workspace.(workspaceQuitPort); ok {
		count = p.QuitDirtyCount()
	} else {
		for _, tab := range m.ed.Tabs() {
			if tab.Modified {
				count++
			}
		}
	}
	if count == 0 {
		return m.quitConfirmed()
	}
	if len(m.large.saves) > 0 && !m.large.quitAfterSave {
		m.large.quitAfterSave = true
		m.notice = "Quit when the save has finished"
		return nil
	}
	m.askUnsaved(flow{then: nextQuit}, fmt.Sprintf("%d modified files", count))
	return nil
}

// quitConfirmed quits at once, or, with editor.confirm_quit on, asks first: the Quit and
// Cancel dialog answers through quitConfirmTag. Without a dialog port the quit is refused
// with a notice, as an unsaved tab is.
func (m *Model) quitConfirmed() tea.Cmd {
	if !m.ed.QuitNeedsConfirm() {
		return m.quit()
	}
	if m.dialogs == nil {
		m.notice = uiText("gvim.notice.quit_unconfirmable", "Quit needs confirmation, and this port cannot ask")
		return nil
	}
	m.pushOverlay(m.dialogs.Quit(quitConfirmTag{}, m.styles.Overlay))
	return nil
}

// saveQuitNext saves each remaining modified tab without asking again.
func (m *Model) saveQuitNext() tea.Cmd {
	t := m.tabPort()
	if t == nil {
		return nil
	}
	for i, tab := range m.ed.Tabs() {
		if !tab.Modified {
			continue
		}
		t.ActivateTab(i)
		m.syncViewport()
		f := flow{then: nextQuit, index: i}
		if !hasPath(m.ed.Active()) {
			m.askSaveAs(f)
			return nil
		}
		return m.saveThen(f)
	}
	return m.quit()
}

// askUnsaved opens the unsaved-changes dialog for the tab called name.
func (m *Model) askUnsaved(f flow, name string) {
	if m.dialogs == nil {
		m.notice = "Unsaved changes in " + name
		return
	}
	m.pushOverlay(m.dialogs.Unsaved(name, unsavedTag{f: f}, m.styles.Overlay))
}

// askSaveAs opens the Save As prompt. It is prefilled with the path of the
// active document, or with a .txt name under the explorer root for an
// untitled one.
func (m *Model) askSaveAs(f flow) {
	if m.dialogs == nil {
		m.notice = "Save As is not supported in this port"
		return
	}
	doc := m.ed.Active()
	if doc == nil {
		return
	}
	prefill := filepath.Join(m.rootDir(), doc.Name()+".txt")
	if p, ok := doc.(pathEditor); ok && p.FilePath() != "" {
		prefill = p.FilePath()
	}
	m.pushOverlay(m.dialogs.SaveAs(prefill, saveAsTag{f: f}, m.styles.Overlay))
}

// dialogDone applies the answer of a file dialog. Each tag names the flow the
// dialog belongs to, and the choice decides the next step. Save As sends Save
// for a new name and Replace once the user confirms an existing one.
func (m *Model) dialogDone(d dialog.Result) tea.Cmd {
	switch tag := d.Tag.(type) {
	case unsavedTag:
		return m.unsavedAnswer(tag.f, d.Choice)
	case quitConfirmTag:
		if d.Choice == dialog.ChoiceQuit {
			return m.quit()
		}
		m.notice = uiText("gvim.notice.quit_cancelled", "Quit cancelled")
		return nil
	case saveAsTag:
		switch d.Choice {
		case dialog.ChoiceSave:
			return m.saveAs(d.Text, false, tag.f)
		case dialog.ChoiceReplace:
			return m.saveAs(d.Text, true, tag.f)
		}
	}
	return nil
}

// unsavedAnswer applies the choice of the unsaved-changes dialog for flow f.
// Save writes the tab, through Save As when it is untitled. Discard closes it
// without saving. Either way the flow goes on, and Cancel ends it.
func (m *Model) unsavedAnswer(f flow, c dialog.Choice) tea.Cmd {
	t := m.tabPort()
	if t == nil {
		return nil
	}
	switch c {
	case dialog.ChoiceSave:
		if f.then == nextQuit {
			if _, ok := m.workspace.(workspaceQuitPort); ok {
				return workspaceQuitNextCmd()
			}
			return m.saveQuitNext()
		}
		if !hasPath(m.ed.Active()) {
			m.askSaveAs(f)
			return nil
		}
		return m.saveThen(f)
	case dialog.ChoiceDiscard:
		if f.then == nextQuit {
			if p, ok := m.workspace.(workspaceQuitPort); ok {
				if err := p.QuitDiscardAll(); err != nil {
					m.notice = err.Error()
					return nil
				}
				return m.quit()
			}
			tabs := m.ed.Tabs()
			for i, tab := range slices.Backward(tabs) {
				if tab.Modified {
					if err := m.closeTab(t, i, true); err != nil {
						m.notice = err.Error()
						return nil
					}
				}
			}
			m.syncViewport()
			return m.quit()
		}
		if err := m.closeTab(t, f.index, true); err != nil {
			m.notice = err.Error()
			return nil
		}
		m.syncViewport()
	}
	return nil
}

// afterSave runs the step that follows a saved tab: the tab closes, or the quit
// loop goes on.
func (m *Model) afterSave(f flow) tea.Cmd {
	switch f.then {
	case nextClose:
		if t := m.tabPort(); t != nil {
			if err := m.closeTab(t, f.index, false); err != nil {
				m.notice = err.Error()
			}
			m.syncViewport()
		}
	case nextQuit:
		if _, ok := m.workspace.(workspaceQuitPort); ok {
			return workspaceQuitNextCmd()
		}
		return m.saveQuitNext()
	}
	return nil
}
