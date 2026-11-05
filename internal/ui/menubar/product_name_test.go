package menubar

import (
	"strings"
	"testing"
)

// TestNoMenuLabelNamesTheLegacyProduct guards the generated table: the menu
// bar is gvim, so no label a user can see may say Fresh.
func TestNoMenuLabelNamesTheLegacyProduct(t *testing.T) {
	var walk func(menu string, items []legacyItem)
	walk = func(menu string, items []legacyItem) {
		for _, it := range items {
			if strings.Contains(it.label, "Fresh") {
				t.Errorf("menu %q: label %q names the legacy product", menu, it.label)
			}
			walk(menu, it.children)
		}
	}
	for _, m := range legacyMenus {
		if strings.Contains(m.label, "Fresh") {
			t.Errorf("menu title %q names the legacy product", m.label)
		}
		walk(m.label, m.items)
	}
}

// TestHelpMenuNamesGvim checks the Help menu's header and help row.
func TestHelpMenuNamesGvim(t *testing.T) {
	for _, m := range legacyMenus {
		if m.label != "Help" {
			continue
		}
		if len(m.items) == 0 || m.items[0].label != "gvim" {
			t.Fatalf("Help header = %+v, want label gvim", m.items)
		}
		found := false
		for _, it := range m.items {
			if it.action == "show_help" && it.label == "Show Help" {
				found = true
			}
		}
		if !found {
			t.Fatal("Help menu has no Show Help row for show_help")
		}
		return
	}
	t.Fatal("no Help menu")
}
