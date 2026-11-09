package tui

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
	"github.com/tbxark/gvim/internal/tui/dock"
)

// TestTask198DisplayStrings checks the update/URL/preview strings fall back to English in
// German (gvim-only keys) and the terminal-restart label uses Fresh's status.terminal_*
// translations, including the exit-code and signal composition. Not parallel: locale.
func TestTask198DisplayStrings(t *testing.T) {
	i18n.SetLocale("de")
	t.Cleanup(func() { i18n.SetLocale("en") })
	if got := uiText("gvim.notice.update_command_missing", "Update command is not configured (update_command)"); got != "Update command is not configured (update_command)" {
		t.Errorf("update notice = %q", got)
	}
	if got := uiTextVars("gvim.url.open_confirm", "Open %{url} in the system browser?", map[string]any{"url": "https://x"}); got != "Open https://x in the system browser?" {
		t.Errorf("URL confirm = %q", got)
	}
	if got := uiText("gvim.palette.preview_unavailable", "Preview unavailable"); got != "Preview unavailable" {
		t.Errorf("preview fallback = %q", got)
	}
	if got := terminalRestartText(dock.RestartInfo{Program: "claude"}); got != "⟳ claude neu starten" {
		t.Errorf("German restart label = %q, want Fresh's translation", got)
	}
	if got := terminalRestartText(dock.RestartInfo{Program: "claude", ResumesAgent: true}); got != "⟳ claude fortsetzen" {
		t.Errorf("German resume label = %q", got)
	}
	if got := terminalRestartText(dock.RestartInfo{Program: "claude", ExitKnown: true, ExitCode: 1}); got != "⟳ claude neu starten (Code 1)" {
		t.Errorf("German exit label = %q", got)
	}
	// The signal suffix is a gvim extension with no Fresh key: English fallback.
	if got := terminalRestartText(dock.RestartInfo{ExitKnown: true, ExitCode: -1}); got != "⟳ Terminal neu starten (signal)" {
		t.Errorf("signal label = %q, want the translated base plus English signal suffix", got)
	}
}
