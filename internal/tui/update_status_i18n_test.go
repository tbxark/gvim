package tui

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestUpdateTextUsesTheCatalogue checks the extracted update indicator: English keeps
// "Update: vX", French uses Fresh's status.update_available translation with %{version}
// interpolated, and no known version keeps the segment absent. Not parallel: locale.
func TestUpdateTextUsesTheCatalogue(t *testing.T) {
	t.Cleanup(func() { i18n.SetLocale("en") })
	m := &Model{updateInfo: &UpdateInfo{Version: "1.2.3"}}
	if got := m.updateText(); got != "Update: v1.2.3" {
		t.Fatalf("English update text = %q", got)
	}
	i18n.SetLocale("fr")
	if got := m.updateText(); got != "Mise à jour : v1.2.3" {
		t.Errorf("French update text = %q, want Fresh's translated status key", got)
	}
	if got := (&Model{}).updateText(); got != "" {
		t.Errorf("empty update text = %q, want no segment", got)
	}
}
