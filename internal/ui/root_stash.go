package ui

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"
)

// Interrupted is one file whose unsaved text an interrupted save kept in a stash file.
type Interrupted struct {
	Path  string // the file, or "" for an untitled buffer
	Stash string // the stash file that holds the unsaved text
}

// name is the label of the file in the review: its base name, or the untitled label.
func (it Interrupted) name() string {
	if it.Path == "" {
		return "untitled buffer"
	}
	return filepath.Base(it.Path)
}

// InterruptedSaves lists the files that have a stash in the session directory, as the
// saved session names them. The app adapter reads the session, so the root calls it
// only from a tea.Cmd.
type InterruptedSaves interface {
	Interrupted() ([]Interrupted, error)
}

// interruptedMsg is the list that the review read.
type interruptedMsg struct {
	items []Interrupted
	err   error
}

// reviewInterrupted returns the command that reads the stashed files. Without a port
// the notice says there is no session directory.
func (m *Model) reviewInterrupted() tea.Cmd {
	p := m.root.ports.Stashes
	if p == nil {
		m.notice = "No session directory, so no interrupted saves to review"
		return nil
	}
	return func() tea.Msg {
		items, err := p.Interrupted()
		return interruptedMsg{items: items, err: err}
	}
}

// reviewed opens the list of the stashed files. Choosing a file opens it, and the
// notice says where its unsaved text is kept.
func (m *Model) reviewed(msg interruptedMsg) {
	switch {
	case msg.err != nil:
		m.notice = msg.err.Error()
		return
	case len(msg.items) == 0:
		m.notice = "No interrupted saves to review"
		return
	}
	rows := make([]listRow, len(msg.items))
	for i, it := range msg.items {
		rows[i] = listRow{left: it.name(), right: filepath.Base(it.Stash), value: it.Stash}
	}
	run := func(v *listView, r listRow) tea.Cmd {
		v.active = false
		for _, it := range msg.items {
			if it.Stash == r.value {
				m.openInterrupted(it)
			}
		}
		return nil
	}
	m.pushOverlay(listOverlay{v: newListView("Interrupted saves", rows, m.listChrome(), run)})
}

// openInterrupted opens the file of a stashed item and names its stash. An untitled
// buffer has no file to open, so the notice is all it gets.
func (m *Model) openInterrupted(it Interrupted) {
	if it.Path == "" {
		m.notice = "Unsaved text of an untitled buffer is kept in " + it.Stash
		return
	}
	m.openFile(it.Path, false)
	m.focusEditor()
	m.notice = "Unsaved text of " + it.name() + " is kept in " + it.Stash
}
