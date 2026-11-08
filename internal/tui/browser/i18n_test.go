package browser

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestBrowserChromeUsesTheCatalogue checks the extracted column headers: English keeps
// the old labels and German uses Fresh's file_browser translations. Not parallel: locale.
func TestBrowserChromeUsesTheCatalogue(t *testing.T) {
	t.Cleanup(func() { i18n.SetLocale("en") })
	if got := sortName.label(); got != "Name" {
		t.Fatalf("English name column = %q", got)
	}
	i18n.SetLocale("de")
	if got := sortSize.label(); got != "Größe" {
		t.Errorf("German size column = %q, want Fresh's translation", got)
	}
	if got := sortModified.label(); got != "Geändert" {
		t.Errorf("German modified column = %q, want Fresh's translation", got)
	}
}
