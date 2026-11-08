package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
)

// The welcome screen shows in the editor pane while no tab is open. It is one
// block centred in the pane: a three-line title, a subtitle, the doors (the
// common actions, each with its shortcut), and the recent folders. A door or a
// recent folder is a row: a click runs it, and Up, Down and Enter move the
// highlight and run it.

// paintWelcome draws the welcome lines of l. The row at index sel is highlighted.
// The screen covers the editor pane, so the explorer is not touched.
func paintWelcome(g *grid.Grid, st style.Styles, l layout, sel int) {
	base, selected := style.Cell(st.Editor), style.Cell(st.Accent)
	text, chip, dim := style.Cell(st.Editor), st.Overlay.Idle, style.Cell(st.Gutter)
	for _, r := range l.Welcome {
		switch r.Role {
		case welcomeBlank:
		case welcomeTitleRow:
			g.Put(r.X, r.Y, g.Width(), r.Text, text)
		case welcomeSubtitle, welcomeHeadingRow:
			g.Put(r.X, r.Y, g.Width(), r.Text, dim)
		case welcomeDoorRow, welcomeRecentRow:
			rowStyle := base
			if r.Item == sel {
				rowStyle = selected
			}
			g.Fill(r.X, r.X+r.W, r.Y, rowStyle)
			g.Put(r.LabelX, r.Y, g.Width(), r.Text, rowStyle)
			keyStyle := chip
			switch {
			case r.Item == sel:
				keyStyle = rowStyle
			case r.Role == welcomeRecentRow:
				keyStyle = dim
			}
			g.Put(r.KeyX, r.Y, g.Width(), r.Key, keyStyle)
		}
	}
}

// welcomeVisible reports whether the welcome screen replaces the editor, which
// is when no tab is open.
func (m *Model) welcomeVisible() bool {
	return m.ed.Active() == nil
}

// runWelcome runs the row at index i of the welcome screen. A door runs its
// action through dispatch, as its shortcut would. A recent folder runs the
// switch-project flow (switchProject) for its path. The open_folder action reaches
// the same flow when the browser picks a folder, so the port opens the folder and
// the tree adopts it.
func (m *Model) runWelcome(i int) tea.Cmd {
	items := frame.WelcomeItems(m.recents)
	if i < 0 || i >= len(items) {
		return nil
	}
	it := items[i]
	if it.Path != "" {
		return m.switchProject(it.Path)
	}
	return m.dispatch(Command{Action: it.Action})
}

// welcomeKey moves the highlight with Up and Down, and runs the highlighted row
// with Enter. It reports whether the key was one of these.
func (m *Model) welcomeKey(name string) (tea.Cmd, bool) {
	n := frame.WelcomeItemCount(m.recents)
	switch name {
	case "Up":
		m.welcomeSel = max(m.welcomeSel-1, 0)
	case "Down":
		m.welcomeSel = min(m.welcomeSel+1, n-1)
	case "Enter":
		return m.runWelcome(m.welcomeSel), true
	default:
		return nil, false
	}
	return nil, true
}
