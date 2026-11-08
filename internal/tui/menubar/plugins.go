package menubar

import "strings"

// Plugin-contributed rows of the menu bar dropdowns (plugin API tier 1a). The generated
// table is the native layer; a plugin's rows are inserted into the dropdown when it opens,
// next to the row they name. A row is a plain action: the host runs it as any menu action,
// and an unknown action reaches the plugin dispatch (Ports.Run), which is where the plugin's
// commands run.
//
// The rows are a whole-set replace per plugin on the host side, so a reload or an unload
// drops the rows of the old generation.

// insertPluginRows inserts the plugin rows of the menu title into the native rows.
func insertPluginRows(native []Row, title string, plugins []PluginRow, holds func(string) bool) []Row {
	var mine []PluginRow
	for _, pr := range plugins {
		if menuMatches(title, pr.Menu) {
			mine = append(mine, pr)
		}
	}
	if len(mine) == 0 {
		return native
	}
	rows := make([]Row, len(native), len(native)+len(mine))
	copy(rows, native)
	for _, pr := range mine {
		row := Row{Label: pr.Label, Action: pr.Action}
		row.Disabled = pr.When != "" && holds != nil && !holds(pr.When)
		row.Checkbox = pr.Checkbox != ""
		row.Checked = row.Checkbox && holds != nil && holds(pr.Checkbox)
		at := len(rows)
		switch {
		case pr.After != "":
			if i := findMenuRow(rows, pr.After); i >= 0 {
				at = i + 1
			}
		case pr.Before != "":
			if i := findMenuRow(rows, pr.Before); i >= 0 {
				at = i
			}
		}
		rows = append(rows, Row{})
		copy(rows[at+1:], rows[at:])
		rows[at] = row
	}
	return rows
}

// menuMatches reports whether a menu's label (as the bar opens it) is the target the plugin
// named: its stable id first, its display label second, case-insensitively.
func menuMatches(label, want string) bool {
	if want == "" {
		return false
	}
	if strings.EqualFold(label, want) {
		return true
	}
	for _, lm := range legacyMenus {
		if lm.label == label {
			return strings.EqualFold(lm.id, want)
		}
	}
	return false
}

// findMenuRow returns the index of the row whose action or label is ref, -1 when none.
func findMenuRow(rows []Row, ref string) int {
	for i, r := range rows {
		if r.Action == ref || r.Label == ref {
			return i
		}
	}
	return -1
}
