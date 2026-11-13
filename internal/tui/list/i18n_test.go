package list

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// The list chrome follows the active locale at the time it is used, not the one at
// package initialisation.
func TestChromeTextFollowsTheActiveLocale(t *testing.T) {
	prev := i18n.Locale()
	t.Cleanup(func() { i18n.SetLocale(prev) })
	i18n.Register("xx", map[string]string{
		"gvim.list.hint":           "hint-xx",
		"gvim.list.read_only_hint": "read-only-xx",
		"gvim.list.filter_label":   "filter-xx: ",
	})
	i18n.SetLocale("xx")

	if got := Hint(); got != "hint-xx" {
		t.Errorf("Hint() = %q, want %q", got, "hint-xx")
	}
	if got := ReadOnlyHint(); got != "read-only-xx" {
		t.Errorf("ReadOnlyHint() = %q, want %q", got, "read-only-xx")
	}
	if got := filterLabel(); got != "filter-xx: " {
		t.Errorf("filterLabel() = %q, want %q", got, "filter-xx: ")
	}
}
