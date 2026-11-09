package dock

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestDockTitlesUseTheCatalogue checks the extracted dock titles/notices: German shows the
// English fallback (gvim-only keys) and the exit-suffix composition stays byte-identical.
// Not parallel: locale.
func TestDockTitlesUseTheCatalogue(t *testing.T) {
	i18n.SetLocale("de")
	t.Cleanup(func() { i18n.SetLocale("en") })
	if got := dockTextVars("gvim.dock.title_exited_suffix", "Terminal (exited, %{suffix})", map[string]any{"suffix": "exit 2"}); got != "Terminal (exited, exit 2)" {
		t.Errorf("exited title = %q", got)
	}
	if got := dockTextVars("gvim.dock.exit_code", "exit %{code}", map[string]any{"code": 2}); got != "exit 2" {
		t.Errorf("exit suffix = %q", got)
	}
	if got := dockTextVars("gvim.dock.bad_pattern", "Bad pattern: %{error}", map[string]any{"error": "x"}); got != "Bad pattern: x" {
		t.Errorf("bad pattern notice = %q", got)
	}
	if got := dockTextVars("gvim.dock.tab_limit_max", "Terminal: the tab limit (%{max}) is reached", map[string]any{"max": 8}); got != "Terminal: the tab limit (8) is reached" {
		t.Errorf("tab limit = %q", got)
	}
	if got := dockTextVars("gvim.dock.hover_open", "Ctrl+Click to open %{token}", map[string]any{"token": "http://x"}); got != "Ctrl+Click to open http://x" {
		t.Errorf("hover notice = %q", got)
	}
	for _, key := range []string{"gvim.dock.tab_limit", "gvim.dock.tab_limit_max", "gvim.dock.hover_open", "gvim.dock.title_numbered", "gvim.dock.title", "gvim.dock.title_exited", "gvim.dock.title_exited_suffix", "gvim.dock.scrollback", "gvim.dock.scrollback_restored", "gvim.dock.scrollback_restored_rewrapped", "gvim.dock.scrollback_exited", "gvim.dock.exit_signal", "gvim.dock.exit_code", "gvim.dock.no_agent", "gvim.dock.shell_start_failed", "gvim.dock.bad_pattern"} {
		if got := i18n.T("en", key, nil); got == key {
			t.Errorf("missing English entry for %s", key)
		}
	}
}
