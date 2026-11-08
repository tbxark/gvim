package tui

import (
	"fmt"
	"path/filepath"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/explorer"
)

// toggleHidden flips the show-hidden setting of the tree and reloads the tree through
// the background reload the file watcher uses, so the listing shows the change.
func (m *Model) toggleHidden() tea.Cmd {
	if m.tree == nil {
		m.notice = "No file explorer"
		return nil
	}
	show := !m.tree.ShowHidden()
	m.tree.SetShowHidden(show)
	if show {
		m.notice = "Hidden files: shown"
	} else {
		m.notice = "Hidden files: hidden"
	}
	return m.reloadTreeAsync()
}

// The status-row messages of an explicit reveal (Fresh's explorer.* strings).
const (
	revealNoFileNotice  = "File explorer focused; this buffer has no file to reveal"
	revealOutsideNotice = "File explorer focused; %s is outside the project"
)

// activeFilePath is the path of the active document, or "" when there is no
// document or it has no file on disk.
func (m *Model) activeFilePath() string {
	doc := m.ed.Active()
	if doc == nil {
		return ""
	}
	return docPath(doc)
}

// pathInTree reports whether path is inside the explorer's root.
func (m *Model) pathInTree(path string) bool {
	if m.tree == nil || path == "" {
		return false
	}
	rel, err := filepath.Rel(m.tree.Root(), path)
	if err != nil {
		return false
	}
	if rel == ".." || filepath.IsAbs(rel) {
		return false
	}
	// A path above the root reads as "../<name>" on every OS.
	return len(rel) < 3 || rel[:2] != ".." || filepath.Separator != rel[2]
}

// revealActiveFile takes the tree to the active file. feedback says the reveal was
// explicit ("show me where I am": the sidebar was shown or focused) and the status
// row must say why it could not move, as Fresh does; a follow is silent. It
// returns the background load command when a folder on the way has not been read
// yet: the listing applies the pending reveal when it lands (revealPending), so
// the update loop still does no file IO.
func (m *Model) revealActiveFile(feedback bool) tea.Cmd {
	if m.tree == nil || !m.explorerOpen {
		return nil
	}
	path := m.activeFilePath()
	if path == "" {
		if feedback {
			m.notice = revealNoFileNotice
		}
		return nil
	}
	if !m.pathInTree(path) {
		if feedback {
			m.notice = fmt.Sprintf(revealOutsideNotice, filepath.Base(path))
		}
		return nil
	}
	revealed := false
	if r, ok := m.tree.(treeRevealer); ok {
		revealed = r.RevealLoaded(path) == nil
	} else {
		revealed = m.tree.Reveal(path) == nil
	}
	if revealed {
		m.followSelection()
		return nil
	}
	// A folder on the way is not loaded: read it in the background and reveal the
	// path when the listing is applied (the watcher's reload does the same).
	return m.reloadToReveal(path)
}

// followActiveBuffer reveals the active file as tabs change, when
// file_explorer.follow_active_buffer is on. It does nothing while the keyboard is
// in the tree (the user is navigating it), while the panel is hidden, or for a file
// outside the root, as Fresh's follow_path_in_explorer.
func (m *Model) followActiveBuffer() tea.Cmd {
	if !m.view.opts.Explorer.FollowActiveBuffer || m.tree == nil || !m.explorerOpen || m.explorerFocus {
		return nil
	}
	return m.revealActiveFile(false)
}

// autoOpenExplorerOnLastClose focuses the explorer when the last tab closed and
// file_explorer.auto_open_on_last_buffer_close is on, as Fresh's buffer_close does.
// The explorer keeps the focus until a buffer opens; explorerAutoOpened makes that
// opening give the keyboard back to the editor.
func (m *Model) autoOpenExplorerOnLastClose() tea.Cmd {
	if !m.view.opts.Explorer.AutoOpenOnLastBufferClose || m.tree == nil {
		return nil
	}
	m.explorerAutoOpened = true
	return m.focusExplorer()
}

// followSelection scrolls the tree so the selected row is in view, under the
// sticky band: the row and the pinned ancestors share the body, so the room the
// run has is the body less the pins (explorer.FollowScroll).
func (m *Model) followSelection() {
	if m.tree == nil || !m.explorerOpen {
		return
	}
	sel, ok := m.tree.Selected()
	if !ok {
		return
	}
	rows := m.tree.Rows()
	idx := slices.IndexFunc(rows, func(r explorer.Row) bool { return r.Path == sel.Path })
	if idx < 0 {
		return
	}
	off := m.tree.ScrollOffset()
	if target := explorer.FollowScroll(rows, idx, off, m.explorerBodyRows()); target != off {
		m.tree.ScrollBy(target - off)
	}
}
