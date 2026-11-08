package tui

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"
)

// watchMsg reports one change in the tree root, from the file watcher. The watcher
// debounces its events, so one message stands for a burst of changes. dirs are the
// directories that changed, as the watcher reports them, and files are the entries that
// changed, by path.
type watchMsg struct {
	dirs  []string
	files []string
}

// watchWait returns the command that waits for the next change in the tree root.
// It returns nil when no watcher is wired. A closed watcher sends nothing, and the
// wait stops there.
func (m *Model) watchWait() tea.Cmd {
	ch := m.root.ports.Watch
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		c, ok := <-ch
		if !ok {
			return nil
		}
		return watchMsg{dirs: c.Dirs, files: c.Files}
	}
}

// watchChanged reloads the tree, as Ctrl+R does, in the background, and reads the git
// status again. It returns the command that waits for the next change.
func (m *Model) watchChanged() tea.Cmd {
	reload := m.reloadTreeAsync()
	return tea.Batch(reload, m.gitCmd(), m.watchWait())
}

// watchSync returns the command that adds the folders of the open documents to the file
// watcher, so that a change to a document outside the tree root still reaches the root. A
// folder is added once: the folders asked for are recorded on the update loop, and the adds run
// in the command. It returns nil when there is no watcher or no folder is new.
func (m *Model) watchSync() tea.Cmd {
	w := m.root.ports.WatchDirs
	if w == nil || m.root.ports.Watch == nil {
		return nil
	}
	var fresh []string
	for _, f := range m.openTabFiles() {
		dir := filepath.Dir(f.path)
		if m.root.watched[dir] {
			continue
		}
		if m.root.watched == nil {
			m.root.watched = make(map[string]bool)
		}
		m.root.watched[dir] = true
		fresh = append(fresh, dir)
	}
	if len(fresh) == 0 {
		return nil
	}
	return func() tea.Msg {
		for _, dir := range fresh {
			// A folder that cannot be watched, such as one that was removed, is skipped.
			_ = w.Add(dir)
		}
		return nil
	}
}
