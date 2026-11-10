package dock

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestNoticesFollowTheLocale checks that the dock's extracted notices are translated when
// the locale changes. Not parallel: it changes and restores the process locale.
func TestNoticesFollowTheLocale(t *testing.T) {
	i18n.Register("xx", map[string]string{
		"terminal.no_terminal_open": "ingen terminal",
		"gvim.terminal.starting":    "startar",
	})
	i18n.SetLocale("xx")
	t.Cleanup(func() {
		i18n.SetLocale("en")
		i18n.Register("xx", nil)
	})
	if got, want := NoTerminalNotice(), "ingen terminal"; got != want {
		t.Fatalf("NoTerminalNotice() = %q, want %q", got, want)
	}
	if got, want := startingShellNotice(), "startar"; got != want {
		t.Fatalf("startingShellNotice() = %q, want %q", got, want)
	}
	i18n.SetLocale("en")
	if got, want := NoTerminalNotice(), "No terminal is open."; got != want {
		t.Fatalf("English NoTerminalNotice() = %q, want %q", got, want)
	}
}

// TestNoticesUseTheEmbeddedGermanCatalogue checks the shipped locale files: with the
// embedded de catalogue active, the dock's terminal notice comes from Fresh's German
// translation (the mapped terminal.no_terminal_open key) with no test registration.
// Not parallel: it changes and restores the process locale.
func TestNoticesUseTheEmbeddedGermanCatalogue(t *testing.T) {
	i18n.SetLocale("de")
	t.Cleanup(func() { i18n.SetLocale("en") })
	want := "Kein offenes Terminal — zuerst ein Terminal öffnen"
	if got := NoTerminalNotice(); got != want {
		t.Fatalf("de NoTerminalNotice() = %q, want %q", got, want)
	}
	// The starting notice is gvim-only: the English fallback shows.
	if got := startingShellNotice(); got != "Starting a shell…" {
		t.Fatalf("de startingShellNotice() = %q, want the English fallback", got)
	}
}
