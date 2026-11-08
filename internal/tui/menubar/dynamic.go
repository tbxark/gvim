package menubar

import "github.com/tbxark/gvim/theme"

// CopyThemePrefix starts the action of a row of the Copy with Formatting submenu:
// the prefix and the theme name. The root runs copy_with_theme with that theme.
const CopyThemePrefix = "copy_with_theme:"

// submenuItems returns the rows a submenu opens. The generated table has no rows
// for the submenus that Fresh builds at run time (DynamicSubmenu), so this
// hand-written layer fills them in: Copy with Formatting lists one row per theme,
// as Fresh's generate_dynamic_items does. Every other submenu keeps its rows.
func submenuItems(it legacyItem) []legacyItem {
	if len(it.children) > 0 || it.label != "Copy with Formatting" {
		return it.children
	}
	var items []legacyItem
	for _, name := range theme.Names() {
		items = append(items, legacyItem{
			kind: itemAction, label: name, action: CopyThemePrefix + name, when: "has_selection",
		})
	}
	return items
}
