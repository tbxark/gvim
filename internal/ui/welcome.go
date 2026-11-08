package ui

import (
	"path/filepath"
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/space"
	"github.com/tbxark/gvim/internal/ui/style"
)

// The welcome screen shows in the editor pane while no tab is open. It is one
// block centred in the pane: a three-line title, a subtitle, the doors (the
// common actions, each with its shortcut), and the recent folders. A door or a
// recent folder is a row: a click runs it, and Up, Down and Enter move the
// highlight and run it.

const (
	welcomeRecentMax    = 5  // the recent folders listed
	welcomeLabelW       = 18 // columns a door's label takes before its key
	welcomeSubtitleText = "a terminal editor"
	welcomeRecent       = "Recent"
)

// welcomeTitle is the three-line ASCII title.
var welcomeTitle = [3]string{
	"  __ __   _(_)_ __ ___",
	" / _` \\ \\ / / | '_ ` _ \\",
	"| (_| |\\ V /| | | | | | |",
}

// welcomeItem is one row that a click or Enter runs: a door, or a recent folder.
// A door runs action with the shortcut key beside it. A recent folder has path
// set and no action.
type welcomeItem struct {
	label  string
	key    string
	action string
	path   string
}

// welcomeDoors are the doors, in order. They carry no keys: the keys come from the active
// keymap (see doorKeys), and a keymap that cannot be asked shows none rather than the keys
// of one preset.
var welcomeDoors = []welcomeItem{
	{label: "New File", action: "new"},
	{label: "Open File", action: "open"},
	{label: "Open Folder", action: "switch_project"},
	{label: "Command Palette", action: "command_palette"},
	{label: "Explorer", action: "focus_file_explorer"},
}

// welcomeItems lists the rows a click or Enter can reach: the doors, then up
// to welcomeRecentMax recent folders. The index of an item is its row index.
func welcomeItems(recents []string) []welcomeItem {
	items := slices.Clone(welcomeDoors)
	for _, p := range recents[:min(len(recents), welcomeRecentMax)] {
		items = append(items, welcomeItem{label: filepath.Base(p), key: p, path: p})
	}
	return items
}

// welcomeRole says how a welcome line is painted.
type welcomeRole int

const (
	welcomeBlank      welcomeRole = iota
	welcomeTitleRow               // a line of the title
	welcomeSubtitle               // the subtitle, centred and dimmed
	welcomeHeadingRow             // the "Recent" heading, dimmed
	welcomeDoorRow                // a door: the key is drawn as an accent chip
	welcomeRecentRow              // a recent folder: the path is drawn dimmed
)

// welcomeLine is one placed line of the welcome screen, in frame cells. item is
// the index of the row in welcomeItems, or -1 for a line that is not a row. keyX
// is the column of the key, or -1 when the line has none.
type welcomeLine struct {
	role   welcomeRole
	x, y   int
	w      int // the row width, which is the width of the highlight
	text   string
	labelX int // a row: the column of the label, inside the highlight's padding
	keyX   int
	key    string
	item   int
}

// welcomeLines places the welcome block in the pane that starts at column x0
// and is paneW cells wide, with rows bodyTop to bodyTop+bodyRows-1. The block is
// centred, and the rows that do not fit are dropped. doorKeys, when it is not nil,
// are the shortcuts of the doors in the active keymap, one per door, and replace the
// keys of welcomeDoors.
func welcomeLines(x0, paneW, bodyTop, bodyRows int, recents []string, doorKeys []string) []welcomeLine {
	if paneW <= 0 {
		return nil // no room: a row's highlight would reach into the explorer
	}
	items := welcomeItems(recents)
	for i, k := range doorKeys {
		if i < len(welcomeDoors) {
			items[i].key = k
		}
	}
	// The block keeps a margin of PadX cells from the pane edge, and a row's highlight
	// reaches PadX cells beyond the text on each side, so a full block is 4*PadX cells
	// narrower than the pane.
	bw := min(welcomeWidth(items), max(paneW-4*space.PadX, 0))
	var rows []welcomeLine // y is the offset from the top of the block
	blank := welcomeLine{role: welcomeBlank, item: -1, keyX: -1}
	for _, t := range welcomeTitle {
		rows = append(rows, welcomeLine{role: welcomeTitleRow, text: t, item: -1, keyX: -1, w: ansi.StringWidth(t)})
	}
	rows = append(rows, blank,
		welcomeLine{role: welcomeSubtitle, text: welcomeSubtitleText, item: -1, keyX: -1, w: ansi.StringWidth(welcomeSubtitleText)},
		blank)
	for i, it := range items {
		if i == len(welcomeDoors) {
			rows = append(rows, blank, welcomeLine{role: welcomeHeadingRow, text: welcomeRecent, item: -1, keyX: -1, w: ansi.StringWidth(welcomeRecent)})
		}
		role := welcomeDoorRow
		if it.path != "" {
			role = welcomeRecentRow
		}
		label := padCells(truncateCells(it.label, welcomeLabelW-1), welcomeLabelW)
		rows = append(rows, welcomeLine{role: role, text: label, keyX: welcomeLabelW, key: fitKey(it, max(bw-welcomeLabelW, 0)), item: i, w: bw + 2*space.PadX})
	}

	top := bodyTop + max((bodyRows-len(rows))/2, 0)
	left := x0 + max((paneW-bw)/2, 0)
	var out []welcomeLine
	for i, r := range rows {
		y := top + i
		if y < bodyTop || y >= bodyTop+bodyRows {
			continue
		}
		r.y = y
		r.x = left
		switch r.role {
		case welcomeTitleRow, welcomeSubtitle:
			r.x = left + max((bw-r.w)/2, 0)
		case welcomeDoorRow, welcomeRecentRow:
			r.x, r.labelX = left-space.PadX, left
			r.keyX += left
		}
		out = append(out, r)
	}
	return out
}

// welcomeWidth is the width of the block before it is fitted to the pane: the
// widest of the title, the doors, and the recent folders' paths.
func welcomeWidth(items []welcomeItem) int {
	w := 0
	for _, t := range welcomeTitle {
		w = max(w, ansi.StringWidth(t))
	}
	for _, it := range items {
		w = max(w, welcomeLabelW+ansi.StringWidth(it.key))
	}
	return w
}

// welcomeItemCount is the number of rows the welcome screen has.
func welcomeItemCount(recents []string) int {
	return len(welcomeItems(recents))
}

// paintWelcome draws the welcome lines of l. The row at index sel is highlighted.
// The screen covers the editor pane, so the explorer is not touched.
func paintWelcome(g *grid.Grid, st style.Styles, l layout, sel int) {
	base, selected := style.Cell(st.Editor), style.Cell(st.Accent)
	text, chip, dim := style.Cell(st.Editor), st.Overlay.Idle, style.Cell(st.Gutter)
	for _, r := range l.welcome {
		switch r.role {
		case welcomeBlank:
		case welcomeTitleRow:
			g.Put(r.x, r.y, g.Width(), r.text, text)
		case welcomeSubtitle, welcomeHeadingRow:
			g.Put(r.x, r.y, g.Width(), r.text, dim)
		case welcomeDoorRow, welcomeRecentRow:
			rowStyle := base
			if r.item == sel {
				rowStyle = selected
			}
			g.Fill(r.x, r.x+r.w, r.y, rowStyle)
			g.Put(r.labelX, r.y, g.Width(), r.text, rowStyle)
			keyStyle := chip
			switch {
			case r.item == sel:
				keyStyle = rowStyle
			case r.role == welcomeRecentRow:
				keyStyle = dim
			}
			g.Put(r.keyX, r.y, g.Width(), r.key, keyStyle)
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
	items := welcomeItems(m.recents)
	if i < 0 || i >= len(items) {
		return nil
	}
	it := items[i]
	if it.path != "" {
		return m.switchProject(it.path)
	}
	return m.dispatch(Command{Action: it.action})
}

// welcomeKey moves the highlight with Up and Down, and runs the highlighted row
// with Enter. It reports whether the key was one of these.
func (m *Model) welcomeKey(name string) (tea.Cmd, bool) {
	n := welcomeItemCount(m.recents)
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

// fitKey fits the right column of a welcome row in w cells. A shortcut is cut at the end,
// and the path of a recent folder gives up its middle and keeps its last directory.
func fitKey(it welcomeItem, w int) string {
	if it.path != "" {
		return space.Path(it.key, w)
	}
	return space.End(it.key, w)
}
