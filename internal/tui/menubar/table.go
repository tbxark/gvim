package menubar

import "strings"

// menuKind classifies a row of the generated menu table.
type menuKind uint8

const (
	itemAction menuKind = iota
	itemSeparator
	itemSubmenu
	itemLabel
)

// legacyItem is one row of the generated table in menu_table_gen.go. when names
// the context key that enables an action; checkbox names the context key for the
// checked state, which the menu bar draws as a box before the label.
type legacyItem struct {
	kind     menuKind
	label    string
	keys     string
	action   string
	when     string
	checkbox string
	children []legacyItem
}

// legacyMenu is one label of the menu bar with its rows.
type legacyMenu struct {
	id, label, when string
	items           []legacyItem
}

// Row is one row of an open dropdown, built from the table for the state at the
// time the dropdown opened. Separators draw a rule. Disabled rows are drawn dim
// and the keyboard skips them. A submenu row opens its children, and one with no
// children and the version label do not open anything.
type Row struct {
	Label     string
	Keys      string // shortcut shown at the right edge, may be empty
	Action    string // action name passed to Ports.Run on activation
	Separator bool
	Submenu   bool
	Disabled  bool
	Checkbox  bool   // the row toggles a setting, drawn with a box before the label
	Checked   bool   // the setting is on
	Opens     string // the bar label whose dropdown the row opens, for the overflow marker
	children  []Row  // the rows of the submenu
}

// selectable reports whether the keyboard can highlight the row.
func (r Row) selectable() bool {
	return !r.Separator && !r.Disabled
}

// barLabels is the menu bar, in order. "Explorer" is listed only while the
// explorer has focus, as the legacy screens show.
func barLabels(explorerFocus bool) []string {
	labels := []string{"File", "Edit", "View", "Selection", "Go", "LSP"}
	if explorerFocus {
		labels = append(labels, "Explorer")
	}
	return append(labels, "Help")
}

// dropdownRows returns the rows of the bar label title, with each action
// enabled when its context key holds. A row shows the shortcut that keyOf gives
// for its action when keyOf is not nil and reports ok, and the shortcut of the
// table otherwise. It returns nil when the table has no such label.
func dropdownRows(title string, holds func(string) bool, keyOf func(string) (string, bool), themeChoices []ThemeChoice) []Row {
	for _, lm := range legacyMenus {
		if lm.label == title {
			return menuRows(lm.items, holds, keyOf, themeChoices)
		}
	}
	return nil
}

// menuRows converts table rows into dropdown rows. An action whose when key does
// not hold is disabled, and a checkbox row is checked when its key holds.
func menuRows(items []legacyItem, holds func(string) bool, keyOf func(string) (string, bool), themeChoices []ThemeChoice) []Row {
	rows := make([]Row, 0, len(items))
	for _, it := range items {
		// The shortcut comes from the active keymap only. The table's own keys are the macOS
		// labels of Fresh's menu, so a row with no keymap to ask shows none.
		row := Row{Label: it.label, Action: childAction(it)}
		switch it.kind {
		case itemSeparator:
			row.Separator = true
		case itemSubmenu:
			row.Submenu = true
			row.Keys = " >  "
			row.children = menuRows(submenuItems(it, themeChoices), holds, keyOf, themeChoices)
		case itemLabel:
			row.Disabled = true
		case itemAction:
			row.Disabled = it.when != "" && !holds(it.when)
			row.Checkbox = it.checkbox != ""
			row.Checked = row.Checkbox && holds(it.checkbox)
			if keyOf != nil {
				row.Keys, _ = keyOf(it.action)
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// keymapActionPrefix names the keymap a Keybinding Style row switches to: the table
// gives all four rows the action switch_keybinding_map, and the checkbox key tells
// them apart.
const keymapActionPrefix = "switch_keybinding_map:"

// childAction is the action a row runs. A Keybinding Style row runs
// "switch_keybinding_map:" and the preset name, so the port knows which one.
func childAction(it legacyItem) string {
	if it.action == "switch_keybinding_map" {
		if name, ok := strings.CutPrefix(it.checkbox, "keymap_"); ok {
			return keymapActionPrefix + strings.ReplaceAll(name, "_", "-")
		}
	}
	return it.action
}
