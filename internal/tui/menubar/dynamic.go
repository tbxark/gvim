package menubar

import "github.com/tbxark/gvim/theme"

// CopyThemePrefix starts the action of a row of the Copy with Formatting submenu:
// the prefix and the theme name. The root runs copy_with_theme with that theme.
const CopyThemePrefix = "copy_with_theme:"

// submenuItems returns the rows a submenu opens. Copy with Formatting lists
// one row per theme dynamically; every other submenu keeps its static rows.
func submenuItems(it menuItem, themeChoices []ThemeChoice) []menuItem {
	if len(it.children) > 0 || it.label != "Copy with Formatting" {
		return it.children
	}
	if len(themeChoices) == 0 {
		for _, name := range theme.Names() {
			themeChoices = append(themeChoices, ThemeChoice{Name: name, Ref: name})
		}
	}
	var items []menuItem
	for _, choice := range themeChoices {
		items = append(items, menuItem{
			kind: itemAction, label: choice.Name, action: CopyThemePrefix + choice.Ref, when: "has_selection",
		})
	}
	return items
}
