package menubar

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
// checked state, which the menu bar does not draw.
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
// and the keyboard skips them. Submenu rows and the version label do not open
// anything.
type Row struct {
	Label     string
	Keys      string // shortcut shown at the right edge, may be empty
	Action    string // action name passed to Ports.Run on activation
	Separator bool
	Submenu   bool
	Disabled  bool
	acc       accel // accelerator letter, chosen from Label
}

// Accel is the lowercase letter that activates the row while the dropdown is
// open, or 0 when no letter of the label is free.
func (r Row) Accel() rune { return r.acc.key }

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
// enabled when its context key holds. It returns nil when the table has no
// such label.
func dropdownRows(title string, holds func(string) bool) []Row {
	for _, lm := range legacyMenus {
		if lm.label == title {
			return menuRows(lm.items, holds)
		}
	}
	return nil
}

// menuRows converts table rows into dropdown rows. An action whose when key does
// not hold is disabled. Each action and submenu row takes the first free letter
// of its label, in table order. Separators and the version label take none.
func menuRows(items []legacyItem, holds func(string) bool) []Row {
	labels := make([]string, len(items))
	for i, it := range items {
		if it.kind == itemAction || it.kind == itemSubmenu {
			labels[i] = it.label
		}
	}
	accels := pickAccels(labels)
	rows := make([]Row, 0, len(items))
	for i, it := range items {
		row := Row{Label: it.label, Keys: it.keys, Action: it.action, acc: accels[i]}
		switch it.kind {
		case itemSeparator:
			row.Separator = true
		case itemSubmenu:
			row.Submenu = true
			row.Keys = " >  "
		case itemLabel:
			row.Disabled = true
		case itemAction:
			row.Disabled = it.when != "" && !holds(it.when)
		}
		rows = append(rows, row)
	}
	return rows
}
