package tui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/ctxmenu"
	"github.com/tbxark/gvim/internal/tui/editorpane"
	expanel "github.com/tbxark/gvim/internal/tui/explorer"
)

// noContextMenuNotice is the status text of a right press on a part of the frame that
// has no context menu.
const noContextMenuNotice = "No context menu here: right-click a tab, the explorer or the editor text"

// showContextMenu opens a context menu with items at the pointer, painted with the
// shared overlay frame of the menus.
func (m *Model) showContextMenu(mo tea.Mouse, items []ctxmenu.Item) {
	menu := ctxmenu.New(m.styles.Overlay)
	menu.Show(mo.X, mo.Y, m.width, m.height, items)
	m.pushOverlay(menu)
}

// rightClick handles a right press. The tab strip, the explorer and the editor text
// have a context menu, as in Fresh (the editor text has none there). Anywhere else
// the status row says that there is none, so the press does not look ignored.
func (m *Model) rightClick(msg tea.MouseClickMsg) tea.Cmd {
	mo := tea.Mouse(msg)
	m.menu = m.menu.Close()
	m.drag = drag{}
	l := m.layout()
	t := l.Hit(mo.X, mo.Y)
	switch t.Kind {
	case hitTab, hitTabClose:
		m.showContextMenu(mo, m.tabMenuItems(t.Index))
	case hitExplorerRow, hitExplorerBody:
		return m.explorerContextMenu(mo, l)
	case hitEditorText:
		m.editorContextMenu(mo, t)
	case hitNone:
	default:
		m.notice = noContextMenuNotice
	}
	return nil
}

// contextChosen runs the row a context menu sent.
func (m *Model) contextChosen(c ctxmenu.Chosen) tea.Cmd {
	switch a := c.Item.Action; {
	case strings.HasPrefix(a, "tab_"):
		i, _ := c.Item.Data.(int)
		return m.tabMenuAction(a, i)
	case strings.HasPrefix(a, explorerMenuPrefix):
		paths, _ := c.Item.Data.([]string)
		return m.explorerMenuAction(strings.TrimPrefix(a, explorerMenuPrefix), paths)
	}
	m.focusEditor()
	return m.dispatch(Command{Action: c.Item.Action})
}

// ---- tabs

// tabMenuItems are the rows of the tab menu of Fresh: close, the batch closes and the
// two path copies. A row with nothing to act on is disabled.
func (m *Model) tabMenuItems(i int) []ctxmenu.Item {
	tabs := m.ed.Tabs()
	hasPath := i >= 0 && i < len(tabs) && tabs[i].Path != ""
	return []ctxmenu.Item{
		{Label: "Close", Action: "tab_close", Data: i},
		{Label: "Close Others", Action: "tab_close_others", Data: i, Disabled: len(tabs) < 2},
		{Label: "Close to the Right", Action: "tab_close_right", Data: i, Disabled: i >= len(tabs)-1},
		{Label: "Close to the Left", Action: "tab_close_left", Data: i, Disabled: i <= 0},
		{Label: "Close All", Action: "tab_close_all", Data: i},
		{Separator: true},
		{Label: "Copy Relative Path", Action: "tab_copy_relative_path", Data: i, Disabled: !hasPath},
		{Label: "Copy Full Path", Action: "tab_copy_full_path", Data: i, Disabled: !hasPath},
	}
}

// plusMenuItems are the rows of the "+" button's menu, as Fresh's: a new terminal, and
// a new file.
func plusMenuItems() []ctxmenu.Item {
	return []ctxmenu.Item{
		{Label: "New Terminal", Action: "open_terminal"},
		{Label: "New File", Action: "new"},
	}
}

// tabMenuAction runs a row of the tab menu for the tab at index i.
func (m *Model) tabMenuAction(action string, i int) tea.Cmd {
	tabs := m.ed.Tabs()
	if i < 0 || i >= len(tabs) {
		return nil
	}
	var targets []int
	switch action {
	case "tab_close":
		return m.requestClose(i)
	case "tab_close_others":
		for j := range tabs {
			if j != i {
				targets = append(targets, j)
			}
		}
	case "tab_close_right":
		for j := i + 1; j < len(tabs); j++ {
			targets = append(targets, j)
		}
	case "tab_close_left":
		for j := range i {
			targets = append(targets, j)
		}
	case "tab_close_all":
		for j := range tabs {
			targets = append(targets, j)
		}
	case "tab_copy_relative_path":
		return m.setClipboard(m.relativePath(tabs[i].Path))
	case "tab_copy_full_path":
		return m.setClipboard(tabs[i].Path)
	}
	m.closeTabs(targets)
	return nil
}

// closeTabs closes the tabs at the indexes in idx, from the last to the first so that
// an index stays right while the ones after it close. A tab with unsaved edits is
// kept, and the status row counts them: closing it needs the question that closing a
// single tab asks.
func (m *Model) closeTabs(idx []int) {
	t := m.tabPort()
	if t == nil {
		m.notice = "Closing tabs is not supported in this port"
		return
	}
	kept := 0
	for _, i := range slices.Backward(idx) {
		needs, err := t.CloseNeedsConfirm(i)
		if err != nil || needs {
			kept++
			continue
		}
		if err := m.closeTab(t, i, false); err != nil {
			kept++
		}
	}
	m.syncViewport()
	if kept > 0 {
		m.notice = fmt.Sprintf("Kept %d tab(s) with unsaved changes: save or close them one by one", kept)
	}
}

// relativePath is path relative to the workspace root, or path itself when it lies
// outside the root or there is no explorer to name one.
func (m *Model) relativePath(path string) string {
	if m.tree == nil {
		return path
	}
	if rel, err := filepath.Rel(m.tree.Root(), path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return rel
	}
	return path
}

// setClipboard puts text on the system clipboard: an OSC 52 write, and a native
// write when the app gave one. The status row says what was copied.
func (m *Model) setClipboard(text string) tea.Cmd {
	m.notice = "Copied " + text
	cmd := tea.SetClipboard(text)
	if w, ok := m.dock.Clipboard().(ClipboardWriter); ok {
		cmd = tea.Batch(cmd, func() tea.Msg {
			_ = w.Write(text)
			return nil
		})
	}
	return cmd
}

// ---- explorer

// explorerContextMenu selects what the pointer is on and opens the explorer menu: the
// full one for a single entry, the short one for several picked entries, and only
// creation and paste for the root and the blank area, as in Fresh.
func (m *Model) explorerContextMenu(mo tea.Mouse, l layout) tea.Cmd {
	if m.tree == nil || !m.explorerOpen {
		return nil
	}
	target, ok := m.explorerPanel.SelectAt(m.explorerGeometry(l), mo.X, mo.Y)
	if !ok {
		return nil
	}
	m.takeExplorerFocus()
	m.showContextMenu(mo, m.explorerMenuItems(target))
	return nil
}

// explorerMenuItems are the rows of the explorer menu for target. Rename acts on one
// entry, so the menu of several entries leaves it out, as Fresh's does; Delete stays and
// trashes them all after one question.
func (m *Model) explorerMenuItems(target expanel.ContextTarget) []ctxmenu.Item {
	paths := target.Paths
	row := func(label, action string) ctxmenu.Item {
		return ctxmenu.Item{Label: label, Action: explorerMenuPrefix + action, Data: paths}
	}
	rule := ctxmenu.Item{Separator: true}
	paste := row("Paste", "paste")
	paste.Disabled = m.clip == nil
	create := []ctxmenu.Item{row("New File", "file_explorer_new_file"), row("New Directory", "file_explorer_new_directory")}
	switch {
	case target.Root || len(paths) == 0:
		return append(create, paste)
	case len(paths) > 1:
		return []ctxmenu.Item{
			row("Cut", "cut"), row("Copy", "copy"), paste, row("Delete", "file_explorer_delete"), row("Duplicate", "duplicate"), rule,
			row("Copy Full Path", "copy_full_path"), row("Copy Relative Path", "copy_relative_path"),
		}
	}
	return append(create,
		row("Rename", "file_explorer_rename"), row("Cut", "cut"), row("Copy", "copy"), paste,
		row("Delete", "file_explorer_delete"), row("Duplicate", "duplicate"), rule,
		row("Copy Full Path", "copy_full_path"), row("Copy Relative Path", "copy_relative_path"),
	)
}

// explorerMenuPrefix starts the action of a row of the explorer menu. The rest is an
// explorer action, or one of duplicate, copy_full_path and copy_relative_path.
const explorerMenuPrefix = "explorer:"

// explorerMenuAction runs a row of the explorer menu. The explorer actions go to the
// panel, which acts on the entries the right press selected.
func (m *Model) explorerMenuAction(action string, paths []string) tea.Cmd {
	switch action {
	case "duplicate":
		jobs := make([]fileJob, 0, len(paths))
		for _, p := range paths {
			jobs = append(jobs, fileJob{kind: jobCopy, path: p, dir: filepath.Dir(p), policy: expanel.ConflictCancel})
		}
		return m.runJobs(jobs)
	case "copy_full_path":
		return m.setClipboard(strings.Join(paths, "\n"))
	case "copy_relative_path":
		rel := make([]string, len(paths))
		for i, p := range paths {
			rel[i] = m.relativePath(p)
		}
		return m.setClipboard(strings.Join(rel, "\n"))
	}
	cmd, _ := m.explorerAction(action)
	return cmd
}

// ---- editor

// editorContextMenu opens the menu of the editor text: the clipboard actions and
// select all. Fresh has no menu there, so this one is the usual short editor menu.
// A press with nothing selected also puts the caret where the pointer is, so Paste
// lands there. A selection is kept, so Cut and Copy act on it, and so is a set of
// several carets, which Paste then fills in at each caret. Cut and Copy still need a
// selection, as they do in Fresh. On a read-only document Cut and Paste are disabled.
func (m *Model) editorContextMenu(mo tea.Mouse, t hitTarget) {
	doc := m.ed.Active()
	if doc == nil {
		return
	}
	_, _, selected := doc.Selection()
	cl, _ := doc.(editorpane.CaretLister)
	multi := cl != nil && len(cl.Cursors()) > 0
	if !selected && !multi {
		m.pressPane(t.Pane, mo.X, mo.Y, 1)
		m.drag = drag{}
	}
	readOnly := m.Holds("read_only")
	m.focusEditor()
	m.showContextMenu(mo, []ctxmenu.Item{
		{Label: "Cut", Action: "cut", Disabled: !selected || readOnly},
		{Label: "Copy", Action: "copy", Disabled: !selected},
		{Label: "Paste", Action: "paste", Disabled: readOnly},
		{Separator: true},
		{Label: "Select All", Action: "select_all"},
	})
}
