package ui

import tea "charm.land/bubbletea/v2"

// gitStatusMsg carries the status letters read for the tree root root. err is set
// when the read failed.
type gitStatusMsg struct {
	root  string
	marks map[string]rune
	err   error
}

// gitCmd returns the command that reads the git status of the tree root, or nil
// when there is no git port or no tree. The read is done by the port in a
// command, so Update never touches git.
func (m *Model) gitCmd() tea.Cmd {
	g := m.root.ports.Git
	if g == nil || m.tree == nil {
		return nil
	}
	dir := m.tree.Root()
	return func() tea.Msg {
		marks, err := g.Marks(dir)
		return gitStatusMsg{root: dir, marks: marks, err: err}
	}
}

// applyGitStatus stores the marks of a read for the current root. A read for a root
// the tree has left is dropped. A failed read clears the marks and shows the error
// in the notice row.
func (m *Model) applyGitStatus(msg gitStatusMsg) {
	if msg.root != m.rootDir() {
		return
	}
	if msg.err != nil {
		m.root.gitMarks = nil
		m.notice = "git status: " + msg.err.Error()
		return
	}
	m.root.gitMarks = msg.marks
}
