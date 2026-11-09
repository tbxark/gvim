package tui

import (
	tea "charm.land/bubbletea/v2"

	expanel "github.com/tbxark/gvim/internal/tui/explorer"
)

// projectOpenedMsg is the result of opening a folder as the project root. root is
// the folder the user picked.
type projectOpenedMsg struct {
	root    string
	project Project
	err     error
}

// switchProject returns the command that opens path as the project root. The port
// does the disk work in that command. Without a port the notice says so.
func (m *Model) switchProject(path string) tea.Cmd {
	p := m.root.ports.Projects
	if p == nil {
		m.notice = "Switch Project is not supported in this port yet"
		return nil
	}
	return func() tea.Msg {
		proj, err := p.Open(path)
		return projectOpenedMsg{root: path, project: proj, err: err}
	}
}

// adoptProject makes the opened project the tree root. The tree keeps the show-hidden
// and show-ignored settings of the old one. The open tabs are not touched. The
// explorer is shown and takes the focus, and the git status of the new root is read.
// A failed open is a notice, and the tree stays as it was.
func (m *Model) adoptProject(msg projectOpenedMsg) tea.Cmd {
	if msg.err != nil {
		m.notice = msg.err.Error()
		return nil
	}
	if msg.project.Tree == nil {
		m.notice = "Open " + msg.root + ": no explorer"
		return nil
	}
	old, prev := m.tree, m.explorerPanel
	tree := msg.project.Tree
	if re, ok := m.ed.(rootEditor); ok {
		re.SetRoot(tree.Root())
	}
	if old != nil {
		tree.SetShowHidden(old.ShowHidden())
		tree.SetShowIgnored(old.ShowIgnored())
	}
	m.tree = tree
	m.resetLiveGrepResume()
	m.explorerPanel = expanel.New(tree, m.styles, m.now)
	if old != nil {
		// The pending cut keeps its dimming, as it does when the panel is hidden.
		m.explorerPanel = m.explorerPanel.CarryCut(prev)
	}
	m.expandRoot()
	if msg.project.Files != nil {
		m.files = msg.project.Files
	}
	m.recents = msg.project.Recents
	m.root.gitMarks = nil
	// The watcher moved to the new root, which removed the old one, so the folders of the
	// documents that stay open are asked again.
	m.root.watched = nil
	m.takeExplorerFocus()
	m.notice = "Project: " + tree.Root()
	// Trust is per workspace: load the new root's decision, and ask when it has
	// none, as Fresh does when another workspace is activated.
	return tea.Batch(m.gitCmd(), m.loadTrust(tree.Root()))
}
