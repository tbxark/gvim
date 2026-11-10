package frame

import (
	"path/filepath"
	"slices"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/space"
)

const (
	WelcomeRecentMax    = 5  // the recent folders listed
	welcomeLabelW       = 18 // columns a door's label takes before its key
	welcomePathMin      = 6  // cells a recent folder's path tail needs to say anything
	WelcomeSubtitleText = "a terminal editor"
	welcomeRecent       = "Recent"
)

// WelcomeTitle is the three-line ASCII title.
var WelcomeTitle = [3]string{
	"  __ __   _(_)_ __ ___",
	" / _` \\ \\ / / | '_ ` _ \\",
	"| (_| |\\ V /| | | | | | |",
}

// WelcomeItem is one row that a click or Enter runs: a door, or a recent folder.
// A door runs action with the shortcut key beside it. A recent folder has path
// set and no action.
type WelcomeItem struct {
	Label  string
	Key    string
	Action string
	Path   string
}

// WelcomeDoors are the doors, in order. They carry no keys: the keys come from the active
// keymap (see doorKeys), and a keymap that cannot be asked shows none rather than the keys
// of one preset.
var WelcomeDoors = []WelcomeItem{
	{Label: "New File", Action: "new"},
	{Label: "Open File", Action: "open"},
	{Label: "Open Folder", Action: "switch_project"},
	{Label: "Command Palette", Action: "command_palette"},
	{Label: "Explorer", Action: "focus_file_explorer"},
}

// WelcomeItems lists the rows a click or Enter can reach: the doors, then up
// to WelcomeRecentMax recent folders. The index of an item is its row index.
func WelcomeItems(recents []string) []WelcomeItem {
	items := slices.Clone(WelcomeDoors)
	for _, p := range recents[:min(len(recents), WelcomeRecentMax)] {
		items = append(items, WelcomeItem{Label: filepath.Base(p), Key: p, Path: p})
	}
	return items
}

// WelcomeRole says how a welcome line is painted.
type WelcomeRole int

const (
	WelcomeBlank      WelcomeRole = iota
	WelcomeTitleRow               // a line of the title
	WelcomeSubtitle               // the subtitle, centred and dimmed
	WelcomeHeadingRow             // the "Recent" heading, dimmed
	WelcomeDoorRow                // a door: the key is drawn as an accent chip
	WelcomeRecentRow              // a recent folder: the path is drawn dimmed
)

// WelcomeLine is one placed line of the welcome screen, in frame cells. item is
// the index of the row in WelcomeItems, or -1 for a line that is not a row. keyX
// is the column of the key, or -1 when the line has none.
type WelcomeLine struct {
	Role   WelcomeRole
	X, Y   int
	W      int // the row width, which is the width of the highlight
	Text   string
	LabelX int // a row: the column of the label, inside the highlight's padding
	KeyX   int
	Key    string
	Item   int
}

// WelcomeLines places the welcome block in the pane that starts at column x0
// and is paneW cells wide, with rows bodyTop to bodyTop+bodyRows-1. The block is
// centred, and the rows that do not fit are dropped. doorKeys, when it is not nil,
// are the shortcuts of the doors in the active keymap, one per door, and replace the
// keys of WelcomeDoors.
func WelcomeLines(x0, paneW, bodyTop, bodyRows int, recents []string, doorKeys []string) []WelcomeLine {
	if paneW <= 0 {
		return nil // no room: a row's highlight would reach into the explorer
	}
	items := WelcomeItems(recents)
	for i, k := range doorKeys {
		if i < len(WelcomeDoors) {
			items[i].Key = k
		}
	}
	// The block keeps a margin of PadX cells from the pane edge, and a row's highlight
	// reaches PadX cells beyond the text on each side, so a full block is 4*PadX cells
	// narrower than the pane.
	bw := min(welcomeWidth(items), max(paneW-4*space.PadX, 0))
	if bw <= 0 {
		return nil
	}
	titleW := 0
	for _, t := range WelcomeTitle {
		titleW = max(titleW, ansi.StringWidth(t))
	}
	// A narrow pane drops the decorative lines rather than clipping them: a cut ASCII
	// wordmark reads as a bug, and the doors still teach the space.
	labelW := min(welcomeLabelW, bw)
	var rows []WelcomeLine // y is the offset from the top of the block
	blank := WelcomeLine{Role: WelcomeBlank, Item: -1, KeyX: -1}
	if titleW <= bw {
		for _, t := range WelcomeTitle {
			rows = append(rows, WelcomeLine{Role: WelcomeTitleRow, Text: t, Item: -1, KeyX: -1, W: ansi.StringWidth(t)})
		}
		rows = append(rows, blank)
	}
	if ansi.StringWidth(WelcomeSubtitleText) <= bw {
		rows = append(rows,
			WelcomeLine{Role: WelcomeSubtitle, Text: WelcomeSubtitleText, Item: -1, KeyX: -1, W: ansi.StringWidth(WelcomeSubtitleText)},
			blank)
	}
	for i, it := range items {
		if i == len(WelcomeDoors) && ansi.StringWidth(welcomeRecent) <= bw {
			rows = append(rows, blank, WelcomeLine{Role: WelcomeHeadingRow, Text: welcomeRecent, Item: -1, KeyX: -1, W: ansi.StringWidth(welcomeRecent)})
		}
		role := WelcomeDoorRow
		if it.Path != "" {
			role = WelcomeRecentRow
		}
		label := space.PadTo(space.End(it.Label, max(labelW-1, 0)), labelW)
		rows = append(rows, WelcomeLine{Role: role, Text: label, KeyX: labelW, Key: welcomeKey(it, max(bw-labelW, 0)), Item: i, W: bw + 2*space.PadX})
	}

	top := bodyTop + max((bodyRows-len(rows))/2, 0)
	left := x0 + max((paneW-bw)/2, 0)
	var out []WelcomeLine
	for i, r := range rows {
		y := top + i
		if y < bodyTop || y >= bodyTop+bodyRows {
			continue
		}
		r.Y = y
		r.X = left
		switch r.Role {
		case WelcomeTitleRow, WelcomeSubtitle:
			r.X = left + max((bw-r.W)/2, 0)
		case WelcomeDoorRow, WelcomeRecentRow:
			r.X, r.LabelX = left-space.PadX, left
			r.KeyX += left
		}
		out = append(out, r)
	}
	return out
}

// welcomeWidth is the width of the block before it is fitted to the pane: the
// widest of the title, the doors, and the recent folders' paths.
func welcomeWidth(items []WelcomeItem) int {
	w := 0
	for _, t := range WelcomeTitle {
		w = max(w, ansi.StringWidth(t))
	}
	for _, it := range items {
		w = max(w, welcomeLabelW+ansi.StringWidth(it.Key))
	}
	return w
}

// WelcomeItemCount is the number of rows the welcome screen has.
func WelcomeItemCount(recents []string) int {
	return len(WelcomeItems(recents))
}

// welcomeKey fits a row's key in budget cells. A door shows its shortcut whole or
// not at all: a cut "Ct…" names nothing. A recent folder abbreviates its path when
// there is room for a useful tail.
func welcomeKey(it WelcomeItem, budget int) string {
	if it.Path != "" {
		if budget < welcomePathMin {
			return ""
		}
		return space.Path(it.Key, budget)
	}
	if ansi.StringWidth(it.Key) > budget {
		return ""
	}
	return it.Key
}
